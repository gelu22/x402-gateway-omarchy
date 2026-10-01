package gateway

// 44.5 money-path adversarial ratchets — named invariants (HANCORE a / 44.1).
// Implementations may share bodies with 44.3 helpers; names are the durable map.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"

	"gateway/internal/policy"
)

// TestPostSignatureSeller500DoesNotReleaseBudget — HANCORE (a) / P0-a.
func TestPostSignatureSeller500DoesNotReleaseBudget(t *testing.T) {
	TestPostSigSeller5xxCommitsBudget(t)
}

// TestPostSignaturePaymentRejectedDoesNotReleaseBudget — HANCORE (a) post-sig 402.
func TestPostSignaturePaymentRejectedDoesNotReleaseBudget(t *testing.T) {
	TestPostSig402CommitsBudget(t)
}

// TestPostSignatureTransportErrorDoesNotReleaseBudget — HANCORE (a) transport after sig.
func TestPostSignatureTransportErrorDoesNotReleaseBudget(t *testing.T) {
	TestPostSigTransportErrorCommitsBudget(t)
}

// TestPreSignatureSignerErrorStillReleases — pre-sig Release regression (44.3).
func TestPreSignatureSignerErrorStillReleases(t *testing.T) {
	TestAuthorizeReleaseOnSignFail(t)
}

// TestContentTooLargeStillCommitted — settle + oversized body still Commits.
func TestContentTooLargeStillCommitted(t *testing.T) {
	TestAuthorizeNoReleaseOnContentTooLarge(t)
}

// TestConcurrentPostSigFailsCommitUnderCap: parallel post-sig 5xx must each
// Commit (never Release) so Today == n*amount under race detector.
func TestConcurrentPostSigFailsCommitUnderCap(t *testing.T) {
	gw, payments := newSettleGateway(t)
	p := policy.Default()
	p.DailyCapMicro = 50_000 // five × 10_000
	p.DomainSubCapPercent = 0
	gw.SetPolicy(p)

	const n = 5
	srv := sellerWith(t, http.StatusInternalServerError)
	var wg sync.WaitGroup
	var failCount atomic.Int32
	var bad atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Distinct paths → distinct reserve keys (avoid 5s dedup window).
			_, err := gw.Fetch(context.Background(), http.MethodGet,
				fmt.Sprintf("%s/c/%d", srv.URL, i), nil, nil)
			if !errors.Is(err, ErrUpstream) {
				bad.Add(1)
				return
			}
			failCount.Add(1)
		}(i)
	}
	wg.Wait()
	if bad.Load() != 0 {
		t.Fatalf("unexpected non-ErrUpstream from %d goroutines", bad.Load())
	}
	if failCount.Load() != n {
		t.Fatalf("failCount = %d, want %d", failCount.Load(), n)
	}
	if payments.Load() != 0 {
		t.Fatalf("OnPayment = %d, want 0 on post-sig fails", payments.Load())
	}
	if got := spendToday(t, gw); got != int64(n)*10_000 {
		t.Fatalf("budget = %d, want %d (each post-sig fail Commits)", got, n*10_000)
	}
}
