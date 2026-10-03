package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"gateway/internal/budget"
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
	// Production mirror (build.go): side effects only. The budget charge, daily
	// and per-domain, is owned by the reservation flow (47.1).
	gw.OnPayment = func(amountMicro int64, domain string) {
		payments.Add(1)
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
	// Per-domain spend is recorded by the budget authority on Commit (47.1).
	spent, err := gw.Budget.DomainTotal("127.0.0.1")
	if err != nil || spent != 50_000 {
		t.Fatalf("domain spend = %d, want 50000 (err %v)", spent, err)
	}
	// Second normal fetch below sub-cap succeeds without asking.
	// (Different path: same URL within the 5s dedup window would be rejected.)
	if _, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content2", nil, nil); err != nil {
		t.Fatalf("known seller below sub-cap must pay, got %v", err)
	}
}

// TestApproveSellerLandFailClosed (NEW-P3-1 / 45.4): Land persist error must
// abort approveSeller — zero Sign/Commit, seller stays unknown.
func TestApproveSellerLandFailClosed(t *testing.T) {
	gw, payments := newSettleGateway(t)
	dir := t.TempDir()
	gw.Sellers = NewSellerRegistry(dir)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	srv := seller402(t, "50000", http.StatusOK)
	_, err := gw.FetchWithOverride(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil, 50_000, true)
	if err == nil {
		t.Fatal("approve with Land fail must err")
	}
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "upstream_error" {
		t.Fatalf("want upstream_error, got %v", err)
	}
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 0 {
		t.Fatalf("signed %d times over failed Land; want 0", got)
	}
	if got := payments.Load(); got != 0 {
		t.Fatalf("payments = %d, want 0", got)
	}
	if known, _ := gw.Sellers.Known("127.0.0.1"); known {
		t.Fatal("failed Land must not mark seller known")
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
	// Pre-load through the authority: the budget ledger is what the domain cap
	// is decided from (47.1).
	commitBudget(t, gw, 15_000, "127.0.0.1")
	// 15k + 30k = 45k > 20k sub-cap, within 100k daily → sharper limit wins.
	srv := seller402(t, "30000", http.StatusOK)
	_, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "domain_cap_exceeded" {
		t.Fatalf("want domain_cap_exceeded (domain cap denied atomically by Authorize, 47.1), got %v", err)
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
	commitBudget(t, gw, 5_000, "127.0.0.1")
	// 5k + 15k = 20k ≤ 20k boundary → pays.
	srv := seller402(t, "15000", http.StatusOK)
	if _, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil); err != nil {
		t.Fatalf("within sub-cap must pay, got %v", err)
	}
	if got := payments.Load(); got != 1 {
		t.Fatalf("payments = %d, want 1", got)
	}
}

// TestSubCapIsPerHostnameNotRegistrableDomain (46.11, F6) pins the T1 boundary:
// the sub-cap is keyed by hostname, so two hosts of the same registrable domain
// each get a full sub-cap. Folding to the registrable domain would need a
// public-suffix list (a new dependency) and would change the policy model, so it
// is deliberately not done. This test makes a future fold a conscious act rather
// than a silent behaviour change.
func TestSubCapIsPerHostnameNotRegistrableDomain(t *testing.T) {
	// The ledger is the budget authority since 47.1; the registry is TOFU-only.
	a := budget.NewAuthority(t.TempDir(), nil)
	subcap := int64(50_000)

	tok, err := a.Authorize(45_000, 1<<62, subcap, "a.example.com")
	if err != nil {
		t.Fatalf("a.example.com: %v", err)
	}
	if err := a.Commit(tok); err != nil {
		t.Fatal(err)
	}
	spentA, err := a.DomainTotal("a.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if spentA != 45_000 {
		t.Fatalf("a.example.com spend = %d, want 45000", spentA)
	}
	// The sibling host has its own budget: no folding to example.com.
	spentB, err := a.DomainTotal("b.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if spentB != 0 {
		t.Fatalf("b.example.com spend = %d, want 0 (per-hostname cap)", spentB)
	}
	// 10k more on the first host would cross 50k; the sibling has room.
	if _, err := a.Authorize(10_000, 1<<62, subcap, "a.example.com"); err != budget.ErrSubCap {
		t.Fatalf("a.example.com must be capped at %d: got %v", subcap, err)
	}
	if _, err := a.Authorize(45_000, 1<<62, subcap, "b.example.com"); err != nil {
		t.Fatalf("b.example.com keeps its own cap: %v", err)
	}
	// The port is not part of the key either (Hostname strips it).
	withPort := normSellerDomain("https://a.example.com:8443/x")
	if withPort != "a.example.com" {
		t.Fatalf("key with port = %q, want a.example.com", withPort)
	}
	// Case folding keeps one bucket per host.
	upper := normSellerDomain("https://A.Example.COM/x")
	if upper != "a.example.com" {
		t.Fatalf("uppercase key = %q, want a.example.com", upper)
	}
}

// TestPermissionLiftsUnknownSellerAndSubCap (49.3): a daemon permission for the
// exact URL lets a normal (non-override) fetch pay without the TOFU ask or the
// per-seller share getting in the way — for every client, not just the panel.
func TestPermissionLiftsUnknownSellerAndSubCap(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir()) // empty → unknown seller
	gw.Permissions = NewPermissionStore(t.TempDir(), nil)
	srv := sellerWith(t, http.StatusOK)
	target := srv.URL + "/content"
	// sellerWith asks 10000; approve up to exactly that.
	if err := gw.Permissions.Add(target, 10_000, false, 0); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := gw.Fetch(context.Background(), http.MethodGet, target, nil, nil); err != nil {
		t.Fatalf("permission must lift the unknown-seller ask, got %v", err)
	}
	if payments.Load() != 1 {
		t.Fatalf("payments = %d, want 1", payments.Load())
	}
}

// TestPermissionAboveCapStillAsks (49.3): a permission does not auto-approve a
// price above its cap; the seller-trust ask comes back.
func TestPermissionAboveCapStillAsks(t *testing.T) {
	gw, _ := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	gw.Permissions = NewPermissionStore(t.TempDir(), nil)
	srv := sellerWith(t, http.StatusOK)
	target := srv.URL + "/content"
	if err := gw.Permissions.Add(target, 5_000, false, 0); err != nil { // below the 10000 ask
		t.Fatalf("Add: %v", err)
	}
	_, err := gw.Fetch(context.Background(), http.MethodGet, target, nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) {
		t.Fatalf("above the cap must ask, got %v", err)
	}
}
