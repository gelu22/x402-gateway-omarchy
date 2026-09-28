package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gateway/internal/gateway"
)

func okResult(body string) *gateway.FetchResult {
	return &gateway.FetchResult{Status: 200, BodyB64: body}
}

// Several identical requests blocked on one MFA challenge must share exactly
// one payment — the whole point of a single pending entry per key.
func TestMfaWaitConcurrentWaitersShareOneRetry(t *testing.T) {
	w := newMfaWait(nil)
	var calls atomic.Int32
	release := make(chan struct{})
	retry := func(context.Context) (*gateway.FetchResult, error) {
		calls.Add(1)
		<-release
		return okResult("paid"), nil
	}

	const n = 8
	entries := make([]*pendingFetch, n)
	released := make([]bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		entries[i] = w.register("k", retry)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			released[i] = w.await(context.Background(), entries[i])
		}(i)
	}

	w.notifyVerified()
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("retries = %d, want exactly 1 for %d identical requests", got, n)
	}
	for i := range released {
		if !released[i] {
			t.Fatalf("waiter %d was not released", i)
		}
		if entries[i].res == nil || entries[i].res.BodyB64 != "paid" {
			t.Fatalf("waiter %d got no result", i)
		}
	}
	if _, ok := w.cached("k"); !ok {
		t.Fatal("the paid result must be cached for a request that arrives later")
	}
}

// No code in the window: nothing is paid, nothing is cached, and the caller
// keeps the original mfa_required error (fail-closed).
func TestMfaWaitTimeoutPaysNothing(t *testing.T) {
	defer func(d time.Duration) { mfaWaitTimeout = d }(mfaWaitTimeout)
	mfaWaitTimeout = 50 * time.Millisecond

	w := newMfaWait(nil)
	var calls atomic.Int32
	e := w.register("k", func(context.Context) (*gateway.FetchResult, error) {
		calls.Add(1)
		return okResult("x"), nil
	})
	if w.await(context.Background(), e) {
		t.Fatal("await must report 'not paid' when no code arrives")
	}
	if calls.Load() != 0 {
		t.Fatal("no retry may run without a verification")
	}
	if _, ok := w.cached("k"); ok {
		t.Fatal("nothing may be cached")
	}
}

func TestMfaWaitCacheTTLAndLimits(t *testing.T) {
	w := newMfaWait(nil)
	base := time.Now()
	w.now = func() time.Time { return base }

	w.cache("k", okResult("cached"))
	if _, ok := w.cached("k"); !ok {
		t.Fatal("fresh entry must hit")
	}
	w.now = func() time.Time { return base.Add(resultTTL) }
	if _, ok := w.cached("k"); ok {
		t.Fatal("entry at the TTL boundary must miss")
	}

	w.now = func() time.Time { return base }
	for i := 0; i < maxCachedResults+3; i++ {
		w.cache(string(rune('a'+i)), okResult("x"))
	}
	w.mu.Lock()
	n := len(w.results)
	w.mu.Unlock()
	if n > maxCachedResults {
		t.Fatalf("cache holds %d entries, want <= %d", n, maxCachedResults)
	}

	w.cache("big", &gateway.FetchResult{Status: 200, BodyB64: strings.Repeat("a", maxCachedBody+1)})
	if _, ok := w.cached("big"); ok {
		t.Fatal("an oversized result must not be cached")
	}
}

// A verification arriving after the wait window must not pay a request whose
// client is long gone (CONTRACTS §1: nothing is paid when no code arrives in
// the window). Audit 30.2b finding (c).
func TestMfaWaitExpiredEntryIsNotPaid(t *testing.T) {
	w := newMfaWait(nil)
	base := time.Now()
	w.now = func() time.Time { return base }
	var calls atomic.Int32
	w.register("k", func(context.Context) (*gateway.FetchResult, error) {
		calls.Add(1)
		return okResult("x"), nil
	})

	w.now = func() time.Time { return base.Add(mfaWaitTimeout + time.Second) }
	w.notifyVerified()
	time.Sleep(20 * time.Millisecond) // let a wrongly spawned retry run
	if calls.Load() != 0 {
		t.Fatal("a verification outside the window must not pay")
	}
	if _, ok := w.cached("k"); ok {
		t.Fatal("nothing may be cached")
	}
}

// One code is consent for one payment: with two distinct requests parked, a
// single verification completes only the newest — the denial the panel shows.
func TestMfaWaitOneCodeCompletesOnePayment(t *testing.T) {
	w := newMfaWait(nil)
	base := time.Now()
	w.now = func() time.Time { return base }
	var calls atomic.Int32
	done := make(chan string, 2)
	mk := func(name string) func(context.Context) (*gateway.FetchResult, error) {
		return func(context.Context) (*gateway.FetchResult, error) {
			calls.Add(1)
			done <- name
			return okResult(name), nil
		}
	}
	w.register("old", mk("old"))
	w.now = func() time.Time { return base.Add(time.Second) }
	w.register("new", mk("new"))

	w.notifyVerified()
	select {
	case got := <-done:
		if got != "new" {
			t.Fatalf("completed %q, want the newest (the one the panel shows)", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no payment was completed")
	}
	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("payments = %d, want exactly 1 per code", calls.Load())
	}
	if _, ok := w.cached("new"); !ok {
		t.Fatal("the completed payment must be cached")
	}
}

// The retry loop rides out the CDP propagation race (F5a): mfa_required until
// the code lands, then success — the entry resolves and the result is cached.
func TestMfaRetryPropagatesAfterVerify(t *testing.T) {
	defer func(d time.Duration) { mfaRetryDelay = d }(mfaRetryDelay)
	mfaRetryDelay = time.Millisecond

	w := newMfaWait(nil)
	var calls atomic.Int32
	e := w.register("k", func(context.Context) (*gateway.FetchResult, error) {
		calls.Add(1)
		if calls.Load() < mfaRetryAttempts {
			return nil, &gateway.PolicyError{Code: "mfa_required", CanOverride: false}
		}
		return okResult("paid"), nil
	})
	w.notifyVerified()
	<-e.done

	if calls.Load() != mfaRetryAttempts {
		t.Fatalf("attempts = %d, want %d (mfa_required until the code propagates)", calls.Load(), mfaRetryAttempts)
	}
	if e.err != nil || e.res == nil || e.res.BodyB64 != "paid" {
		t.Fatalf("entry = (res %v, err %v), want the paid result", e.res, e.err)
	}
	if _, ok := w.cached("k"); !ok {
		t.Fatal("the paid result must be cached")
	}
}

// The code never propagates: after the last attempt the entry keeps
// mfa_required (fail-closed) and nothing is cached.
func TestMfaRetryExhausted(t *testing.T) {
	defer func(d time.Duration) { mfaRetryDelay = d }(mfaRetryDelay)
	mfaRetryDelay = time.Millisecond

	w := newMfaWait(nil)
	var calls atomic.Int32
	e := w.register("k", func(context.Context) (*gateway.FetchResult, error) {
		calls.Add(1)
		return nil, &gateway.PolicyError{Code: "mfa_required", CanOverride: false}
	})
	w.notifyVerified()
	<-e.done

	if calls.Load() != mfaRetryAttempts {
		t.Fatalf("attempts = %d, want %d", calls.Load(), mfaRetryAttempts)
	}
	if mapError(e.err) != "mfa_required" {
		t.Fatalf("entry error = %v, want mfa_required", e.err)
	}
	if _, ok := w.cached("k"); ok {
		t.Fatal("nothing may be cached")
	}
}

// Only mfa_required is retried: any other error stops after exactly one attempt.
func TestMfaRetryOnlyOnMfaRequired(t *testing.T) {
	w := newMfaWait(nil)
	var calls atomic.Int32
	e := w.register("k", func(context.Context) (*gateway.FetchResult, error) {
		calls.Add(1)
		return nil, gateway.ErrSigner
	})
	w.notifyVerified()
	<-e.done

	if calls.Load() != 1 {
		t.Fatalf("attempts = %d, want exactly 1 (signer errors are not retried)", calls.Load())
	}
	if !errors.Is(e.err, gateway.ErrSigner) {
		t.Fatalf("entry error = %v, want signer_error", e.err)
	}
}
