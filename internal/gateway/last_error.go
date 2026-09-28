// Dedup and error tracking for Gateway fetches (CONTRACTS §1).
package gateway

import (
	"time"
)

// setLastFetchError records the last fetch error for UI consumption.
func (g *Gateway) setLastFetchError(code string, amountMicro int64, canOverride bool, targetURL, detail string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	amountUSDC := float64(amountMicro) / 1_000_000
	g.lastFetchError = &FetchErrorInfo{
		Code:        code,
		AmountMicro: amountMicro,
		AmountUSDC:  amountUSDC,
		CanOverride: canOverride,
		TargetURL:   targetURL,
		Timestamp:   g.clock().Format(time.RFC3339Nano),
	}
}

// lastError returns the last fetch error under the same lock its writers use.
// Status() polls concurrently with in-flight fetches (the panel polls /status
// while agents fetch), so the read cannot be lock-free.
func (g *Gateway) lastError() *FetchErrorInfo {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastFetchError
}

// reservation is the opaque token of an in-flight claim. A named type (not a
// bare uint64) so it cannot be mixed up with a timestamp, an index or any other
// counter in this package. Zero means "no reservation" (the counter starts at
// 1) and release refuses it explicitly, so it cannot free a settled key.
type reservation uint64

// signMark is the per-key state of the money-path single-flight guard: when the
// key was last settled (at) and which in-flight reservation owns it (seq; 0 =
// none). seq, not the timestamp, decides ownership — a test clock can be frozen
// and two events would then compare equal.
type signMark struct {
	at  time.Time
	seq reservation
}

// maxSignMarks bounds the dedup map of a long-lived daemon: entries older than
// the window are dropped when the map grows past this (paid marks older than
// the window no longer block anything, so sweeping them is safe).
const maxSignMarks = 1024

// reserve claims a request key for the duration of one fetch.
//
// The old guard was check-then-act (checked here, marked only after signing),
// so two concurrent identical requests — or one /fetch and one /fetch-override
// for the same target — could both reach the signer and pay twice (audit 30.2b).
// Reserving first makes the claim atomic; release undoes it when a flow ends
// without settling, so a retry after a denial still works.
func (g *Gateway) reserve(key string) (reservation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lastSign == nil {
		g.lastSign = map[string]signMark{}
	}
	now := g.clock()
	if m, ok := g.lastSign[key]; ok && now.Sub(m.at) < dedupWindow {
		return 0, ErrDuplicate
	}
	if len(g.lastSign) > maxSignMarks {
		for k, v := range g.lastSign {
			if now.Sub(v.at) >= dedupWindow {
				delete(g.lastSign, k)
			}
		}
	}
	g.resSeq++
	g.lastSign[key] = signMark{at: now, seq: g.resSeq}
	return g.resSeq, nil
}

// release drops a reservation that did not settle. A paid flow keeps its mark
// (markSigned stores seq 0), so a post-payment failure still blocks a retry.
func (g *Gateway) release(key string, seq reservation) {
	// 0 is the mark left by markSigned (a settled key), not a reservation:
	// releasing with it would free a PAID key and let the payment happen twice.
	// Unreachable today (reserve never returns 0), but the guard makes the
	// property hold for any future caller.
	if seq == 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if m, ok := g.lastSign[key]; ok && m.seq == seq {
		delete(g.lastSign, key)
	}
}

func (g *Gateway) markSigned(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lastSign == nil {
		g.lastSign = map[string]signMark{}
	}
	g.lastSign[key] = signMark{at: g.clock()}
}
