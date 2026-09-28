package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"gateway/internal/policy"
)

// 35.4: the override path is the only one that skips the daily budget, so its
// edges must be pinned. Own file: override_integration_test.go is 165 LOC and
// changed Go files are capped at 200. Only the gaps go here — price_changed on
// the override path (settle_test.go:175), non-positive/invalid amounts, TOFU
// approval and sub-cap bypass by approval are already covered.

func policyErrCode(t *testing.T, err error) (*PolicyError, string) {
	t.Helper()
	var perr *PolicyError
	if !errors.As(err, &perr) {
		t.Fatalf("want *PolicyError, got %v", err)
	}
	return perr, perr.Code
}

// Gap (a): a known balance below the amount is refused on the override path too
// — an explicit approval cannot turn "no funds" into a payment.
func TestOverrideInsufficientFundsIsNotOverridable(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Balance = fetcherAt(t, fakeBalanceRPC(t, "0x0")) // 0 USDC

	_, err := gw.FetchWithOverride(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusOK).URL+"/content", nil, nil, 10_000, false)

	perr, code := policyErrCode(t, err)
	if code != "insufficient_funds" || perr.CanOverride {
		t.Fatalf("code=%s canOverride=%v, want non-overridable insufficient_funds", code, perr.CanOverride)
	}
	if payments.Load() != 0 {
		t.Fatalf("payments = %d, want 0", payments.Load())
	}
	if s, _ := gw.Spend.Today(); s != 0 {
		t.Fatalf("spend = %d, want 0", s)
	}
	if gw.lastError() == nil || gw.lastError().Code != "insufficient_funds" {
		t.Fatalf("last_fetch_error = %+v, want insufficient_funds", gw.lastError())
	}
}

// Boundary of gap (a): balance exactly equal to the amount must NOT be blocked
// (the check is "<", not "<=").
func TestOverrideWithExactBalanceSettles(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Balance = fetcherAt(t, fakeBalanceRPC(t, "0x2710")) // 10000 micro = 0.01 USDC

	if _, err := gw.FetchWithOverride(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusOK).URL+"/content", nil, nil, 10_000, false); err != nil {
		t.Fatalf("balance == amount must settle, got %v", err)
	}
	if payments.Load() != 1 {
		t.Fatalf("payments = %d, want 1", payments.Load())
	}
}

// Gap (b): the real order is budget -> TOFU -> sub-cap (fetch.go evaluateSeller),
// so an UNKNOWN seller over the sub-cap gets unknown_seller first; the sub-cap
// binds from the next payment (and an explicit approval lands + pays, which is
// the documented fail-closed rule, pinned by TestOverrideBypassesDomainSubCap).
func TestTOFUPrecedesSubCapForUnknownSeller(t *testing.T) {
	gw, payments := newSettleGateway(t)
	p := policy.Default()
	p.DailyCapMicro = 5_000_000
	p.DomainSubCapPercent = 1 // $0.05 sub-cap
	gw.SetPolicy(p)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	url := sellerAsking(t, "60000", http.StatusOK).URL + "/content" // $0.06 > sub-cap

	_, err := gw.Fetch(context.Background(), http.MethodGet, url, nil, nil)
	if _, code := policyErrCode(t, err); code != "unknown_seller" {
		t.Fatalf("unknown seller over the sub-cap must ask first, got %s", code)
	}
	if payments.Load() != 0 {
		t.Fatalf("payments = %d, want 0", payments.Load())
	}

	// Land it, then the sub-cap is what refuses (known seller, $0.045 already spent).
	if err := gw.Sellers.Land("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	if err := gw.Sellers.Add("127.0.0.1", 45_000); err != nil {
		t.Fatal(err)
	}
	_, err = gw.Fetch(context.Background(), http.MethodGet, url, nil, nil)
	perr, code := policyErrCode(t, err)
	if code != "domain_cap_exceeded" || !perr.CanOverride {
		t.Fatalf("known seller over the sub-cap: code=%s canOverride=%v", code, perr.CanOverride)
	}
	if payments.Load() != 0 {
		t.Fatalf("payments = %d, want 0 over the sub-cap", payments.Load())
	}
}

// Gap (c): the dedup key is METHOD+URL, not the approved amount, so two parallel
// overrides with DIFFERENT amounts still buy once — the loser is a duplicate, not
// a price_changed (nothing about the seller's price changed).
func TestConcurrentOverridesPayOnce(t *testing.T) {
	gw, payments := newSettleGateway(t)
	url := sellerAsking(t, "10000", http.StatusOK).URL + "/content"

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, amount := range []int64{10_000, 20_000} {
		wg.Add(1)
		go func(i int, amount int64) {
			defer wg.Done()
			_, errs[i] = gw.FetchWithOverride(context.Background(), http.MethodGet, url, nil, nil, amount, false)
		}(i, amount)
	}
	wg.Wait()

	if payments.Load() != 1 {
		t.Fatalf("payments = %d, want exactly 1", payments.Load())
	}
	successes := 0
	for i, err := range errs {
		if err == nil {
			successes++
			continue
		}
		if code := auditErrorCode(err); code != "duplicate_payment" {
			t.Fatalf("loser %d code = %q, want duplicate_payment (err=%v)", i, code, err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want 1", successes)
	}
}

// Gap (d): the CDP TEE ceiling is not lifted by an explicit approval — the
// override path surfaces the same non-overridable policy_violation as Fetch.
func TestPolicyViolationOnOverrideIsNotOverridable(t *testing.T) {
	gw, payments := newSettleGateway(t)
	fakeSign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorType":"policy_violation","errorMessage":"policy rejected the signature"}`))
	}))
	t.Cleanup(fakeSign.Close)
	gw.Client.BaseURL = fakeSign.URL

	_, err := gw.FetchWithOverride(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusOK).URL+"/content", nil, nil, 10_000, false)

	perr, code := policyErrCode(t, err)
	if code != "policy_violation" || perr.CanOverride {
		t.Fatalf("code=%s canOverride=%v, want non-overridable policy_violation", code, perr.CanOverride)
	}
	if payments.Load() != 0 {
		t.Fatalf("payments = %d, want 0", payments.Load())
	}
	if s, _ := gw.Spend.Today(); s != 0 {
		t.Fatalf("spend = %d, want 0", s)
	}
}
