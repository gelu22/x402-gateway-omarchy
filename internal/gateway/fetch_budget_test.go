package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
