package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"gateway/internal/budget"
	"gateway/internal/cdp"
	"gateway/internal/policy"
)

// Production charge primitive under test (lint anchor for money-plane gate).
var _ = "Budget.Authorize"

// TestAuthorizeFailClosedNeverSigns: Authorize write failure → fetch fails,
// zero signer calls (r10 fail-closed).
func TestAuthorizeFailClosedNeverSigns(t *testing.T) {
	gw, _ := newSettleGateway(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	gw.Budget = budget.NewAuthority(dir, nil)

	_, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusOK).URL+"/content", nil, nil)
	if err == nil {
		t.Fatal("want error on Authorize failure")
	}
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 0 {
		t.Fatalf("signed %d times after Authorize fail, want 0", got)
	}
}

// failingWSSigner fails WalletSecret so signing aborts after Authorize.
type failingWSSigner struct {
	base  *settleSigner
	calls atomic.Int32
}

func (s *failingWSSigner) Address() string              { return s.base.Address() }
func (s *failingWSSigner) UserID() string               { return s.base.UserID() }
func (s *failingWSSigner) AccessToken() (string, error) { return s.base.AccessToken() }
func (s *failingWSSigner) WalletSecret() (*cdp.WalletSecret, error) {
	s.calls.Add(1)
	return nil, fmt.Errorf("injected wallet secret failure")
}

// TestAuthorizeReleaseOnSignFail: Authorize OK → sign fail → Release restores budget.
func TestAuthorizeReleaseOnSignFail(t *testing.T) {
	gw, payments := newSettleGateway(t)
	failing := &failingWSSigner{base: gw.Signer.(*settleSigner)}
	gw.Signer = failing

	_, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusOK).URL+"/content", nil, nil)
	if err == nil {
		t.Fatal("want signer error")
	}
	if failing.calls.Load() == 0 {
		t.Fatal("signer never reached (Authorize may have failed first)")
	}
	if payments.Load() != 0 {
		t.Fatalf("OnPayment = %d, want 0", payments.Load())
	}
	if got := spendToday(t, gw); got != 0 {
		t.Fatalf("budget after Release = %d, want 0", got)
	}
}

// TestAuthorizeCommitOnSettle: Authorize OK → 2xx → Commit (Today == amount).
func TestAuthorizeCommitOnSettle(t *testing.T) {
	gw, payments := newSettleGateway(t)
	if _, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusOK).URL+"/content", nil, nil); err != nil {
		t.Fatalf("want success, got %v", err)
	}
	if payments.Load() != 1 {
		t.Fatalf("OnPayment = %d, want 1", payments.Load())
	}
	if got := spendToday(t, gw); got != 10_000 {
		t.Fatalf("budget after Commit = %d, want 10000", got)
	}
}

// TestAuthorizeConcurrentCapOne: two different targets, cap = one payment →
// one settles, the other gets budget_exceeded (r10 concurrency).
func TestAuthorizeConcurrentCapOne(t *testing.T) {
	gw, _ := newSettleGateway(t)
	p := policy.Default()
	p.DailyCapMicro = 10_000  // exactly one 10_000 payment
	p.DomainSubCapPercent = 0 // httptest shares 127.0.0.1; isolate daily-cap race
	gw.SetPolicy(p)

	srvA := sellerWith(t, http.StatusOK)
	srvB := sellerWith(t, http.StatusOK)
	var wg sync.WaitGroup
	var okCount, exceedCount atomic.Int32
	for _, u := range []string{srvA.URL + "/a", srvB.URL + "/b"} {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			_, err := gw.Fetch(context.Background(), http.MethodGet, url, nil, nil)
			if err == nil {
				okCount.Add(1)
				return
			}
			var perr *PolicyError
			if errors.As(err, &perr) && perr.Code == "budget_exceeded" {
				exceedCount.Add(1)
				return
			}
			t.Errorf("unexpected err: %v", err)
		}(u)
	}
	wg.Wait()
	if okCount.Load() != 1 || exceedCount.Load() != 1 {
		t.Fatalf("ok=%d exceed=%d, want 1 and 1", okCount.Load(), exceedCount.Load())
	}
	if got := spendToday(t, gw); got != 10_000 {
		t.Fatalf("budget = %d, want 10000", got)
	}
}

// TestAuthorizeNoReleaseOnContentTooLarge: settle OK → Commit, no Release.
func TestAuthorizeNoReleaseOnContentTooLarge(t *testing.T) {
	gw, payments := newSettleGateway(t)
	_, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerBigContent(t, "10000", 2*maxSellerContentBytes).URL+"/content", nil, nil)
	if !errors.Is(err, ErrContentTooLarge) {
		t.Fatalf("want ErrContentTooLarge, got %v", err)
	}
	if payments.Load() != 1 {
		t.Fatalf("OnPayment = %d, want 1", payments.Load())
	}
	if got := spendToday(t, gw); got != 10_000 {
		t.Fatalf("budget = %d, want 10000 (no Release after settle)", got)
	}
}

// TestOnPaymentDoesNotCallSpendAdd: production hook must not touch Spend.
// Charge path is Budget.Authorize → Commit; OnPayment is telemetry/cache only.
func TestOnPaymentDoesNotCallSpendAdd(t *testing.T) {
	gw, _ := newSettleGateway(t)
	var hookCalls atomic.Int32
	gw.OnPayment = func(int64, string) { hookCalls.Add(1) } // production hook: no spend write
	if _, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusOK).URL+"/content", nil, nil); err != nil {
		t.Fatalf("want success, got %v", err)
	}
	if hookCalls.Load() != 1 {
		t.Fatalf("OnPayment = %d, want 1", hookCalls.Load())
	}
	if got := spendToday(t, gw); got != 10_000 {
		t.Fatalf("Budget.Today = %d, want 10000 (Commit owns the ledger)", got)
	}
}

// --- 44.3 HANCORE (a): post-sig outcomes Commit; pre-sig still Releases -----

// TestPostSigSeller5xxCommitsBudget: Payment-Signature sent → seller 500 →
// Budget.Today == amount (Commit), OnPayment == 0.
func TestPostSigSeller5xxCommitsBudget(t *testing.T) {
	gw, payments := newSettleGateway(t)
	_, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusInternalServerError).URL+"/content", nil, nil)
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("want ErrUpstream, got %v", err)
	}
	if payments.Load() != 0 {
		t.Fatalf("OnPayment = %d, want 0", payments.Load())
	}
	if got := spendToday(t, gw); got != 10_000 {
		t.Fatalf("budget = %d, want 10000 (Commit after sig)", got)
	}
}

// TestPostSig402CommitsBudget: signed retry returns 402 → Commit, no OnPayment.
func TestPostSig402CommitsBudget(t *testing.T) {
	gw, payments := newSettleGateway(t)
	_, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusPaymentRequired).URL+"/content", nil, nil)
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("want ErrUpstream, got %v", err)
	}
	if payments.Load() != 0 {
		t.Fatalf("OnPayment = %d, want 0", payments.Load())
	}
	if got := spendToday(t, gw); got != 10_000 {
		t.Fatalf("budget = %d, want 10000 (Commit after sig)", got)
	}
}

// TestPostSigTransportErrorCommitsBudget: connection drop after header → Commit.
func TestPostSigTransportErrorCommitsBudget(t *testing.T) {
	gw, payments := newSettleGateway(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Payment-Signature") != "" {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("ResponseWriter is not a Hijacker")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatalf("hijack: %v", err)
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Payment-Required", paymentRequiredHeaderWith("10000", usdcBaseSepolia, "eip155:84532"))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)

	_, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil)
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("want ErrUpstream on transport drop, got %v", err)
	}
	if payments.Load() != 0 {
		t.Fatalf("OnPayment = %d, want 0", payments.Load())
	}
	if got := spendToday(t, gw); got != 10_000 {
		t.Fatalf("budget = %d, want 10000 (Commit after sig transport err)", got)
	}
}

// --- 44.repass.1 NEW-P1-3: domain spend on every post-sig Commit -------------

func TestPostSig5xxChargesDomainSpend(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	if err := gw.Sellers.Land("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	_, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusInternalServerError).URL+"/content", nil, nil)
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("want ErrUpstream, got %v", err)
	}
	if payments.Load() != 0 {
		t.Fatalf("OnPayment = %d, want 0 (telemetry stays 2xx-only)", payments.Load())
	}
	if got := spendToday(t, gw); got != 10_000 {
		t.Fatalf("budget = %d, want 10000", got)
	}
	dom, err := gw.Budget.DomainTotal("127.0.0.1")
	if err != nil || dom != 10_000 {
		t.Fatalf("domain spend = %d, want 10000 after post-sig 5xx (err %v)", dom, err)
	}
}

func TestPostSig2xxChargesDomainOnce(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	if err := gw.Sellers.Land("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	_, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerWith(t, http.StatusOK).URL+"/content", nil, nil)
	if err != nil {
		t.Fatalf("want success, got %v", err)
	}
	if payments.Load() != 1 {
		t.Fatalf("OnPayment = %d, want 1", payments.Load())
	}
	dom, err := gw.Budget.DomainTotal("127.0.0.1")
	if err != nil || dom != 10_000 {
		t.Fatalf("domain spend = %d, want 10000 once (no double) (err %v)", dom, err)
	}
}

func TestPostSig5xxDomainSubCapTightens(t *testing.T) {
	gw, _ := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	p := policy.Default()
	p.DailyCapMicro = 100_000 // sub-cap 20_000
	gw.SetPolicy(p)
	if err := gw.Sellers.Land("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	// First signed payment asks 15k, seller 500 → Commit + domain charge.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Payment-Signature") != "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Payment-Required", paymentRequiredHeaderWith("15000", usdcBaseSepolia, "eip155:84532"))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)
	_, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/a", nil, nil)
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("first: want ErrUpstream, got %v", err)
	}
	dom, _ := gw.Budget.DomainTotal("127.0.0.1")
	if dom != 15_000 {
		t.Fatalf("domain after first = %d, want 15000", dom)
	}
	// Second 10k would be 25k > 20k sub-cap → denied before signing.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Payment-Required", paymentRequiredHeaderWith("10000", usdcBaseSepolia, "eip155:84532"))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv2.Close)
	_, err = gw.Fetch(context.Background(), http.MethodGet, srv2.URL+"/b", nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "domain_cap_exceeded" {
		t.Fatalf("want domain_cap_exceeded (domain cap after post-sig charge), got %v", err)
	}
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 1 {
		t.Fatalf("signCalls = %d, want 1 (the denied request must not sign)", got)
	}
}

// TestConcurrentRequestsToOneSellerRespectDomainCap (47.1, r12) is the
// maintainer's report at the gateway boundary: two ordinary concurrent requests
// to DIFFERENT urls on one approved seller must not exceed the domain cap. The
// committed total and the reservation have to be read in the same transaction —
// when the committed total came from a separate store, the first payment had
// already left Reserved and was invisible to the second check.
func TestConcurrentRequestsToOneSellerRespectDomainCap(t *testing.T) {
	gw, _ := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	p := policy.Default()
	p.DailyCapMicro = 1_000_000 // 20% sub-cap = 200_000
	gw.SetPolicy(p)
	if err := gw.Sellers.Land("127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	// 3 × 80_000 = 240_000 > 200_000, but any 2 fit — so a correct ledger lets
	// exactly two through and a split ledger would let all three.
	newSeller := func() *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Payment-Signature") == "" {
				w.Header().Set("Payment-Required", paymentRequiredHeaderWith("80000", usdcBaseSepolia, "eip155:84532"))
				w.WriteHeader(http.StatusPaymentRequired)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}))
		t.Cleanup(srv.Close)
		return srv
	}
	a, b, c := newSeller(), newSeller(), newSeller()

	const n = 3
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	urls := []string{a.URL + "/x", b.URL + "/y", c.URL + "/z"}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = gw.Fetch(context.Background(), http.MethodGet, urls[i], nil, nil)
		}(i)
	}
	close(start)
	wg.Wait()

	paid := 0
	for i, err := range errs {
		if err == nil {
			paid++
			continue
		}
		var perr *PolicyError
		if !errors.As(err, &perr) || perr.Code != "domain_cap_exceeded" {
			t.Fatalf("request %d: want domain_cap_exceeded, got %v", i, err)
		}
	}
	if paid != 2 {
		t.Fatalf("paid = %d, want 2 (160_000 fits under 200_000, 240_000 does not)", paid)
	}
	dom, err := gw.Budget.DomainTotal("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if dom != 160_000 {
		t.Fatalf("domain total = %d, want 160000", dom)
	}
}
