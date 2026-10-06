package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway/internal/budget"
	"gateway/internal/policy"
)

// sellerAsking answers the unsigned hit with a 402 naming amount, and the
// signed retry with retryStatus. Lets a test pick the seller's price.
func sellerAsking(t *testing.T, amount string, retryStatus int) *httptest.Server {
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

// TestOverrideFlowBudgetExceededThenApprovedPays walks the panel's real flow:
// a fetch denied by the daily budget changes nothing, the user's approval pays.
func TestOverrideFlowBudgetExceededThenApprovedPays(t *testing.T) {
	gw, payments := newSettleGateway(t)
	tok, err := gw.Budget.Authorize(budget.Hold{AmountMicro: 4_995_000, Domain: "seed"}, budget.Caps{DailyMicro: 5_000_000, DomainMicro: 0}) // $4.995 of the $5 cap
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.Budget.Commit(tok); err != nil {
		t.Fatal(err)
	}
	url := sellerWith(t, http.StatusOK).URL + "/content"

	_, err = gw.Fetch(context.Background(), http.MethodGet, url, nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "budget_exceeded" {
		t.Fatalf("want budget_exceeded, got %v", err)
	}
	if !perr.CanOverride || perr.AmountMicro != 10_000 {
		t.Fatalf("want overridable 10000, got %+v", perr)
	}
	if got := payments.Load(); got != 0 {
		t.Fatalf("OnPayment = %d on denial, want 0", got)
	}
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 0 {
		t.Fatalf("signed %d times on denial, want 0", got)
	}
	if gw.Blocks.Current() == nil {
		t.Fatal("denial must be recorded as a block")
	}

	res, err := gw.FetchWithOverride(context.Background(), http.MethodGet, url, nil, nil, 10_000, false)
	if err != nil {
		t.Fatalf("approved override: %v", err)
	}
	if res.Status != http.StatusOK || payments.Load() != 1 {
		t.Fatalf("want settled override, got status=%d payments=%d", res.Status, payments.Load())
	}
	if got := spendToday(t, gw); got != 5_005_000 {
		t.Fatalf("spend = %d, want 5005000", got)
	}
	if gw.Blocks.Current() != nil {
		t.Fatal("settled override must clear the block")
	}
}

// TestFetchWithOverrideRejectsNonPositiveAmount pins the sanity guard (45.7):
// amount 0 without approveSeller, and any negative, fail before network I/O.
func TestFetchWithOverrideRejectsNonPositiveAmount(t *testing.T) {
	gw, _ := newSettleGateway(t)
	url := sellerWith(t, http.StatusOK).URL + "/content"

	for _, amount := range []int64{0, -1} {
		if _, err := gw.FetchWithOverride(context.Background(), http.MethodGet, url, nil, nil, amount, false); err == nil {
			t.Fatalf("override %d without approve: want error, got nil", amount)
		}
	}
	if _, err := gw.FetchWithOverride(context.Background(), http.MethodGet, url, nil, nil, -1, true); err == nil {
		t.Fatal("negative amount with approve must err")
	}
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 0 {
		t.Fatalf("signed %d times on invalid override, want 0", got)
	}
}

// TestApproveSellerAmountZeroPaysWithinCap (45.7): amount 0 + approve lands and
// pays under the normal daily cap (no MaxInt64 lift).
func TestApproveSellerAmountZeroPaysWithinCap(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	url := sellerAsking(t, "10000", http.StatusOK).URL + "/content"

	if _, err := gw.FetchWithOverride(context.Background(), http.MethodGet, url, nil, nil, 0, true); err != nil {
		t.Fatalf("amount0+approve within cap: %v", err)
	}
	if payments.Load() != 1 {
		t.Fatalf("payments = %d, want 1", payments.Load())
	}
	known, err := gw.Sellers.Known("127.0.0.1")
	if err != nil || !known {
		t.Fatalf("seller must be landed, known=%v err=%v", known, err)
	}
	if got := spendToday(t, gw); got != 10_000 {
		t.Fatalf("spend = %d, want 10000 (normal Authorize, not skipped)", got)
	}
}

// TestApproveSellerAmountZeroRespectsDailyCap (45.7): amount 0 must NOT lift
// the daily cap — over-budget → budget_exceeded, zero Sign.
func TestApproveSellerAmountZeroRespectsDailyCap(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	p := policy.Default()
	p.DailyCapMicro = 5_000
	gw.SetPolicy(p)
	url := sellerAsking(t, "10000", http.StatusOK).URL + "/content"

	_, err := gw.FetchWithOverride(context.Background(), http.MethodGet, url, nil, nil, 0, true)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "budget_exceeded" {
		t.Fatalf("want budget_exceeded (no MaxInt64 lift), got %v", err)
	}
	if !perr.CanOverride {
		t.Fatal("budget_exceeded must remain overridable")
	}
	if payments.Load() != 0 {
		t.Fatalf("payments = %d, want 0", payments.Load())
	}
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 0 {
		t.Fatalf("signed %d times over cap, want 0", got)
	}
	if known, _ := gw.Sellers.Known("127.0.0.1"); !known {
		t.Fatal("Land runs before Authorize — seller should be known after approve attempt")
	}
}

// TestFetchRejectsInvalidAmounts keeps the strict decimal rule (016.6a) on the
// seller-supplied amount: non-positive and scientific notation are denials.
func TestFetchRejectsInvalidAmounts(t *testing.T) {
	for _, amount := range []string{"0", "1e9"} {
		t.Run(amount, func(t *testing.T) {
			gw, _ := newSettleGateway(t)
			_, err := gw.Fetch(context.Background(), http.MethodGet, sellerAsking(t, amount, http.StatusOK).URL+"/content", nil, nil)
			var perr *PolicyError
			if !errors.As(err, &perr) || perr.Code != "invalid_amount" {
				t.Fatalf("amount %q: want invalid_amount, got %v", amount, err)
			}
			if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 0 {
				t.Fatalf("amount %q: signed %d times, want 0", amount, got)
			}
		})
	}
}

// TestOverrideLandsUnknownSeller proves the explicit approval is what admits a
// first-time seller (TOFU), and that the bare fetch never pays it.
func TestOverrideLandsUnknownSeller(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	url := sellerWith(t, http.StatusOK).URL + "/content"

	_, err := gw.Fetch(context.Background(), http.MethodGet, url, nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "unknown_seller" {
		t.Fatalf("want unknown_seller, got %v", err)
	}
	if payments.Load() != 0 {
		t.Fatalf("OnPayment = %d for unknown seller, want 0", payments.Load())
	}

	if _, err := gw.FetchWithOverride(context.Background(), http.MethodGet, url, nil, nil, 10_000, true); err != nil {
		t.Fatalf("approved unknown seller: %v", err)
	}
	if payments.Load() != 1 {
		t.Fatalf("OnPayment = %d, want 1", payments.Load())
	}
	known, err := gw.Sellers.Known("127.0.0.1")
	if err != nil || !known {
		t.Fatalf("seller known = %v (err %v), want landed", known, err)
	}
}

// TestOverrideBypassesDomainSubCap: a known seller over its daily share is
// denied, and only a fresh explicit approval pays past the sub-cap (the
// panel's fail-closed rule, enforced here at the money boundary).
func TestOverrideBypassesDomainSubCap(t *testing.T) {
	gw, payments := newSettleGateway(t)
	p := policy.Default()
	p.DailyCapMicro = 5_000_000
	p.DomainSubCapPercent = 1 // 1% of $5 = $0.05 sub-cap
	gw.SetPolicy(p)

	gw.Sellers = NewSellerRegistry(t.TempDir())
	if err := gw.Sellers.Land("127.0.0.1"); err != nil { // known, so sub-cap is evaluated
		t.Fatal(err)
	}
	url := sellerAsking(t, "60000", http.StatusOK).URL + "/content" // $0.06 > sub-cap

	_, err := gw.FetchWithOverride(context.Background(), http.MethodGet, url, nil, nil, 60_000, false)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "domain_cap_exceeded" {
		t.Fatalf("want domain_cap_exceeded (domain cap denied atomically by Authorize, 47.1), got %v", err)
	}
	if payments.Load() != 0 {
		t.Fatalf("OnPayment = %d over sub-cap, want 0", payments.Load())
	}

	if _, err := gw.FetchWithOverride(context.Background(), http.MethodGet, url, nil, nil, 60_000, true); err != nil {
		t.Fatalf("explicit approval must pay past the sub-cap: %v", err)
	}
	if payments.Load() != 1 {
		t.Fatalf("OnPayment = %d, want 1", payments.Load())
	}
}
