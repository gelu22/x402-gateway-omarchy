// Paid-result cache for the MFA wait path (30.2b). Split from mfa_wait.go: the
// wait is coordination, this is retention — and both stay under the 200 LOC rule.
//
// Memory only, never disk, never the log: a paid body may be sensitive, and the
// only reason to keep it is that the client which paid may have given up before
// the human typed the code (its timeout is not ours to control). An identical
// retry inside resultTTL then gets the content instead of duplicate_payment —
// the wallet was already charged exactly once.
package server

import (
	"time"

	"gateway/internal/gateway"
)

// var, not const: tests shorten it; production never changes it.
var resultTTL = 5 * time.Minute

const (
	// maxCachedResults / maxCachedBody bound the cache (oldest-first eviction).
	maxCachedResults = 8
	maxCachedBody    = 1 << 20
)

type cachedResult struct {
	res *gateway.FetchResult
	at  time.Time
}

// cache stores a paid result in memory only (never disk, never the log);
// oversized bodies are skipped with a Warn — the caller still gets them.
func (w *mfaWait) cache(key string, res *gateway.FetchResult) {
	if res == nil {
		return
	}
	if len(res.BodyB64) > maxCachedBody {
		w.logger.Warn("mfa wait: result too large to cache", "bytes", len(res.BodyB64))
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.results) >= maxCachedResults {
		oldestKey, oldest := "", time.Time{}
		for k, v := range w.results {
			if oldestKey == "" || v.at.Before(oldest) {
				oldestKey, oldest = k, v.at
			}
		}
		delete(w.results, oldestKey)
	}
	w.results[key] = cachedResult{res: res, at: w.now()}
}

// cached returns a paid result still inside resultTTL. The pointer is shared
// between waiters and the cache: callers must treat it as read-only.
func (w *mfaWait) cached(key string) (*gateway.FetchResult, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	r, ok := w.results[key]
	if !ok || w.now().Sub(r.at) >= resultTTL {
		return nil, false
	}
	return r.res, true
}
