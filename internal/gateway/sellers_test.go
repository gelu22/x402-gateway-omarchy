package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gateway/internal/policy"
)

func TestNormSellerDomain(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://Seller.Example.com/api/x402", "seller.example.com"},
		{"https://example.com:8443/pay", "example.com"},
		{"http://[::1]:8080/x", "::1"},
		{"http://[2001:db8::1]:443/x", "2001:db8::1"},
		{"not a url \x7f", ""},
		{"", ""},
		{"https://", ""},
	}
	for _, tc := range cases {
		if got := normSellerDomain(tc.in); got != tc.want {
			t.Errorf("normSellerDomain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSellersLandKnown(t *testing.T) {
	r := NewSellerRegistry(t.TempDir())
	known, err := r.Known("seller.example.com")
	if err != nil || known {
		t.Fatalf("fresh registry: known=%v err=%v, want false/nil", known, err)
	}
	if err := r.Land("seller.example.com"); err != nil {
		t.Fatal(err)
	}
	known, err = r.Known("seller.example.com")
	if err != nil || !known {
		t.Fatalf("after land: known=%v err=%v, want true/nil", known, err)
	}
	// Idempotent: landing twice keeps the original firstSeen.
	if err := r.Land("seller.example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestSellersLandEmptyRefused(t *testing.T) {
	r := NewSellerRegistry(t.TempDir())
	if err := r.Land(""); err == nil {
		t.Fatal("want error landing empty domain")
	}
	if known, _ := r.Known(""); known {
		t.Fatal("empty domain must never be known")
	}
}

func TestSellersDayRollover(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	r := &SellerRegistry{stateDir: dir, now: func() time.Time { return day1 }}
	if err := r.Land("seller.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("seller.example.com", 50000); err != nil {
		t.Fatal(err)
	}
	// Next day: still known (no re-approval), spend counter reset.
	r.now = func() time.Time { return day1.Add(26 * time.Hour) }
	known, err := r.Known("seller.example.com")
	if err != nil || !known {
		t.Fatalf("known must survive day change: %v %v", known, err)
	}
	spent, err := r.Today("seller.example.com")
	if err != nil || spent != 0 {
		t.Fatalf("day spend after rollover = %d, want 0 (err %v)", spent, err)
	}
}

func TestSellersCorruptFresh(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sellers.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewSellerRegistry(dir)
	known, err := r.Known("seller.example.com")
	if err != nil || known {
		t.Fatalf("corrupt file: known=%v err=%v, want false/nil (fail-closed)", known, err)
	}
}

func TestSellersFilePerms(t *testing.T) {
	dir := t.TempDir()
	r := NewSellerRegistry(dir)
	if err := r.Land("seller.example.com"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "sellers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("sellers.json perms %v, want 0600", perm)
	}
}

// seller402 answers the unsigned hit with valid 402 requirements for amount
// and the signed retry with retryStatus.
func seller402(t *testing.T, amount string, retryStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Payment-Signature") != "" {
			w.WriteHeader(retryStatus)
			_, _ = w.Write([]byte("x"))
			return
		}
		w.Header().Set("Payment-Required", paymentRequiredHeaderWith(amount, usdcBaseSepolia, "eip155:84532"))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestUnknownSellerAsks(t *testing.T) {
	gw, _ := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	srv := seller402(t, "50000", http.StatusOK)

	_, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "unknown_seller" {
		t.Fatalf("want unknown_seller, got %v", err)
	}
	if !perr.CanOverride || perr.AmountMicro != 50_000 {
		t.Fatalf("want overridable 50000, got %+v", perr)
	}
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 0 {
		t.Fatalf("signed %d times before seller approval", got)
	}
	// Denial lands nothing: still unknown afterwards.
	if known, _ := gw.Sellers.Known("127.0.0.1"); known {
		t.Fatal("denial must not land the seller")
	}
	// Silent override path (remembered auto-pay) must also ask, never pay.
	_, err = gw.FetchWithOverride(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil, 50_000, false)
	if !errors.As(err, &perr) || perr.Code != "unknown_seller" {
		t.Fatalf("silent override must ask too, got %v", err)
	}
}

func TestApproveSellerLandsAndPays(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	// Full production mirror (main.go): spend + per-domain spend on settle.
	gw.OnPayment = func(amountMicro int64, domain string) {
		payments.Add(1)
		if err := gw.Sellers.Add(domain, amountMicro); err != nil {
			t.Errorf("sellers add: %v", err)
		}
	}
	srv := seller402(t, "50000", http.StatusOK)

	if _, err := gw.FetchWithOverride(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil, 50_000, true); err != nil {
		t.Fatalf("approved override must pay, got %v", err)
	}
	if got := payments.Load(); got != 1 {
		t.Fatalf("payments = %d, want 1", got)
	}
	known, err := gw.Sellers.Known("127.0.0.1")
	if err != nil || !known {
		t.Fatalf("approval must land the seller: %v %v", known, err)
	}
	// Per-domain spend recorded through the production OnPayment wiring.
	spent, err := gw.Sellers.Today("127.0.0.1")
	if err != nil || spent != 50_000 {
		t.Fatalf("domain spend = %d, want 50000 (err %v)", spent, err)
	}
	// Second normal fetch below sub-cap succeeds without asking.
	// (Different path: same URL within the 5s dedup window would be rejected.)
	if _, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content2", nil, nil); err != nil {
		t.Fatalf("known seller below sub-cap must pay, got %v", err)
	}
}

func TestSubCapBlocks(t *testing.T) {
	gw, _ := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	p := policy.Default()
	p.DailyCapMicro = 100_000 // sub-cap 20_000
	gw.SetPolicy(p)
	if err := gw.Sellers.Land("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := gw.Sellers.Add("127.0.0.1", 15_000); err != nil {
		t.Fatal(err)
	}
	// 15k + 30k = 45k > 20k sub-cap, within 100k daily → sharper limit wins.
	srv := seller402(t, "30000", http.StatusOK)
	_, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "budget_exceeded" {
		t.Fatalf("want budget_exceeded (domain sub-cap via Authorize), got %v", err)
	}
	if !perr.CanOverride || perr.AmountMicro != 30_000 {
		t.Fatalf("want overridable 30000, got %+v", perr)
	}
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 0 {
		t.Fatalf("signed %d times over sub-cap", got)
	}
}

func TestSubCapAllows(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	p := policy.Default()
	p.DailyCapMicro = 100_000 // sub-cap 20_000
	gw.SetPolicy(p)
	if err := gw.Sellers.Land("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := gw.Sellers.Add("127.0.0.1", 5_000); err != nil {
		t.Fatal(err)
	}
	// 5k + 15k = 20k ≤ 20k boundary → pays.
	srv := seller402(t, "15000", http.StatusOK)
	if _, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil); err != nil {
		t.Fatalf("within sub-cap must pay, got %v", err)
	}
	if got := payments.Load(); got != 1 {
		t.Fatalf("payments = %d, want 1", got)
	}
}

// Tampered negative day-spend must read as zero, never loosen the sub-cap.
func TestTamperedNegativeDaySpendClamped(t *testing.T) {
	dir := t.TempDir()
	day := time.Now().Format("2006-01-02") // file day == today: no rollover to hide behind
	raw := `{"day":"` + day + `","domains":{"evil.example":{"firstSeen":"` + day + `","day":"` + day + `","daySpendMicro":-9000}}}`
	if err := os.WriteFile(filepath.Join(dir, "sellers.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &SellerRegistry{stateDir: dir, now: func() time.Time { d, _ := time.Parse("2006-01-02", day); return d }}
	v, err := r.Today("evil.example")
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Fatalf("negative daySpend must read zero, got %d", v)
	}
}

// Future-dated entry keeps spend (no silent reset), normalizes the day.
func TestFutureEntryKeepsSpend(t *testing.T) {
	dir := t.TempDir()
	tomorrow := time.Now().Add(26 * time.Hour).Format("2006-01-02")
	raw := `{"day":"` + tomorrow + `","domains":{"evil.example":{"firstSeen":"` + tomorrow + `","day":"` + tomorrow + `","daySpendMicro":31000}}}`
	if err := os.WriteFile(filepath.Join(dir, "sellers.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewSellerRegistry(dir)
	v, err := r.Today("evil.example")
	if err != nil {
		t.Fatal(err)
	}
	if v != 31000 {
		t.Fatalf("future-dated spend must be kept, got %d", v)
	}
}

// Malformed day sorts above any date: reset, never pin spend forever.
func TestMalformedSellerDayResets(t *testing.T) {
	dir := t.TempDir()
	raw := `{"day":"zzz","domains":{"evil.example":{"firstSeen":"zzz","day":"zzz","daySpendMicro":31000}}}`
	if err := os.WriteFile(filepath.Join(dir, "sellers.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewSellerRegistry(dir)
	v, err := r.Today("evil.example")
	if err != nil {
		t.Fatal(err)
	}
	if v != 0 {
		t.Fatalf("malformed day must reset, got %d", v)
	}
}

// Case-variant dup keys collapse: sub-cap accounting cannot be split.
func TestSellerKeysNormalized(t *testing.T) {
	dir := t.TempDir()
	day := time.Now().Format("2006-01-02")
	raw := `{"day":"` + day + `","domains":{"Evil.Example":{"firstSeen":"` + day + `","day":"` + day + `","daySpendMicro":7000}}}`
	if err := os.WriteFile(filepath.Join(dir, "sellers.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewSellerRegistry(dir)
	v, err := r.Today("evil.example")
	if err != nil {
		t.Fatal(err)
	}
	if v != 7000 {
		t.Fatalf("normalized key must read 7000, got %d", v)
	}
}

// Add with non-positive amounts is refused, like spend.Add.
func TestSellerAddRejectsNonPositive(t *testing.T) {
	r := NewSellerRegistry(t.TempDir())
	for _, amount := range []int64{0, -100} {
		if err := r.Add("evil.example", amount); err == nil {
			t.Fatalf("Add(%d) must fail, got nil", amount)
		}
	}
}

// Hand-edited case variants of one domain merge by summing: splitting spend
// across keys must never loosen the sub-cap.
func TestCaseVariantKeysMergeBySum(t *testing.T) {
	dir := t.TempDir()
	day := time.Now().Format("2006-01-02")
	raw := `{"day":"` + day + `","domains":{` +
		`"Evil.Example":{"firstSeen":"` + day + `","day":"` + day + `","daySpendMicro":7000},` +
		`"evil.example":{"firstSeen":"` + day + `","day":"` + day + `","daySpendMicro":3000}}}`
	if err := os.WriteFile(filepath.Join(dir, "sellers.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewSellerRegistry(dir)
	v, err := r.Today("EVIL.EXAMPLE")
	if err != nil {
		t.Fatal(err)
	}
	if v != 10000 {
		t.Fatalf("merged spend = %d, want 10000", v)
	}
}
