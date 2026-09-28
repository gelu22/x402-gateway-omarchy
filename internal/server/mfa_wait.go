// MFA wait for the fetch path (30.2b).
//
// CDP asking for a code at signing time used to lose the payment: the daemon
// returned mfa_required and forgot the request. The code now COMPLETES that
// payment (owner decision, B2-open): the request waits for the verification
// and the daemon retries it. The retry runs in the background so identical
// requests share one payment and a request whose client already left still
// completes — its result lands in the paid-result cache (mfa_result_cache.go).
package server

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"gateway/internal/gateway"
)

// var, not const: tests shorten it; production never changes it.
var mfaWaitTimeout = 180 * time.Second

// mfaWaitGrace pads the socket WriteTimeout so a waiting handler is never cut off mid-wait.
const mfaWaitGrace = 30 * time.Second

const mfaRetryAttempts = 3

var mfaRetryDelay = 2 * time.Second

// pendingFetch is a request blocked on MFA. The retry closure is the handler's
// own gateway call, so /fetch and /fetch-override retry the right operation.
type pendingFetch struct {
	key       string
	started   time.Time
	deadline  time.Time
	retried   bool
	abandoned bool
	done      chan struct{}
	res       *gateway.FetchResult
	err       error
	retry     func(context.Context) (*gateway.FetchResult, error)
}

// mfaWait coordinates "the request waits for a code" and the paid-result cache.
// One instance per Server; every method is safe for concurrent use.
type mfaWait struct {
	mu      sync.Mutex
	pending map[string]*pendingFetch
	results map[string]cachedResult
	now     func() time.Time
	logger  *slog.Logger
}

// mfaWait returns the process-wide wait registry and cache. Lazily created so a
// zero-value Server (tests) works without extra wiring.
func (s *Server) mfaWait() *mfaWait {
	s.mfaMu.Lock()
	defer s.mfaMu.Unlock()
	if s.mfa == nil {
		s.mfa = newMfaWait(s.logger)
	}
	return s.mfa
}

func newMfaWait(logger *slog.Logger) *mfaWait {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &mfaWait{
		pending: map[string]*pendingFetch{},
		results: map[string]cachedResult{},
		now:     time.Now,
		logger:  logger,
	}
}

// register returns the pending entry for key, creating it when absent:
// identical concurrent requests share one retry and therefore one payment.
func (w *mfaWait) register(key string, retry func(context.Context) (*gateway.FetchResult, error)) *pendingFetch {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pruneLocked()
	if e, ok := w.pending[key]; ok {
		return e
	}
	now := w.now()
	e := &pendingFetch{key: key, started: now, deadline: now.Add(mfaWaitTimeout), done: make(chan struct{}), retry: retry}
	w.pending[key] = e
	return e
}

// await blocks until the entry's retry finished. Returns false when nothing was
// paid (the window closed or the client went away) — the caller keeps the
// original mfa_required error, so the outcome stays fail-closed.
func (w *mfaWait) await(ctx context.Context, e *pendingFetch) bool {
	remaining := e.deadline.Sub(w.now())
	if remaining <= 0 {
		w.expire(e)
		return false
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-e.done:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		w.expire(e)
		return false
	}
}

// expire forgets an entry past its window: a verification arriving later must
// not pay a request whose client is long gone (CONTRACTS §1).
func (w *mfaWait) expire(e *pendingFetch) {
	w.mu.Lock()
	defer w.mu.Unlock()
	e.abandoned = true
	if cur, ok := w.pending[e.key]; ok && cur == e {
		delete(w.pending, e.key)
	}
}

// lookup returns the in-flight entry for key, if any.
func (w *mfaWait) lookup(key string) (*pendingFetch, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	e, ok := w.pending[key]
	return e, ok
}

// notifyVerified completes the payment the code was asked for, exactly once.
//
// One code completes ONE payment: the newest denial is the one the panel shows,
// so a code is consent for what the human actually saw (a burst of distinct
// blocked URLs must not be paid by a single prompt). Entries past their window
// are dropped, never paid. The retry runs in the background so a client that
// already left still gets its result cached.
func (w *mfaWait) notifyVerified() {
	w.mu.Lock()
	now := w.now()
	var chosen *pendingFetch
	for k, e := range w.pending {
		if e.retried || e.abandoned || now.Sub(e.started) >= mfaWaitTimeout {
			delete(w.pending, k)
			continue
		}
		if chosen == nil || e.started.After(chosen.started) {
			chosen = e
		}
	}
	if chosen != nil {
		chosen.retried = true
	}
	w.mu.Unlock()
	if chosen != nil {
		go w.retry(chosen)
	}
}

func (w *mfaWait) retry(e *pendingFetch) {
	ctx, cancel := context.WithTimeout(context.Background(), mfaWaitTimeout)
	defer cancel()
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil || e.retry == nil {
			e.err = context.Canceled
			break
		}
		e.res, e.err = e.retry(ctx)
		if e.err == nil || mapError(e.err) != "mfa_required" || attempt >= mfaRetryAttempts {
			break
		}
		w.logger.Info("mfa retry", "attempt", attempt+1, "key", e.key)
		select {
		case <-ctx.Done():
			e.err = context.Canceled
		case <-time.After(mfaRetryDelay):
		}
	}
	if e.err == nil {
		w.cache(e.key, e.res)
	}
	close(e.done)
	w.mu.Lock()
	if cur, ok := w.pending[e.key]; ok && cur == e {
		delete(w.pending, e.key)
	}
	w.mu.Unlock()
}

// pruneLocked drops abandoned or timed-out entries (a blocked waiter times out).
func (w *mfaWait) pruneLocked() {
	for k, e := range w.pending {
		if e.abandoned || w.now().Sub(e.started) >= mfaWaitTimeout {
			delete(w.pending, k)
		}
	}
}
