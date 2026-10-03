// Blocked-request store (49.2): the replacement for the 30.2b wait.
//
// A payment the daemon refuses because it needs the owner (a CDP MFA code, or
// an overridable policy denial) used to make the request WAIT for the code.
// That wait is gone: the request returns immediately, and what it needed is
// remembered here so the panel can show it and the owner can act later.
//
// Memory only, never disk: a blocked request may carry a body and headers, and
// keeping those on disk would add a credential-at-rest surface for no gain. A
// daemon restart loses the list — the agent can always ask again, and losing it
// is fail-closed (nothing is paid from a remembered request without the owner).
package server

import (
	"errors"
	"sync"
	"time"

	"gateway/internal/gateway"
)

// blockedMax bounds the list (oldest-first eviction).
const blockedMax = 16

// blockedTTL drops entries the owner did not act on. Long enough to sit down
// and look, short enough that a stale price is not approved by reflex.
const blockedTTL = 30 * time.Minute

// blockedNotifyWindow coalesces desktop notifications: at most one per window,
// so a burst of refused agent calls cannot flood the desktop.
const blockedNotifyWindow = 60 * time.Second

// BlockedRequest is one refused payment, kept so the owner can approve it.
type BlockedRequest struct {
	ID          string
	Method      string
	URL         string
	Body        []byte
	Headers     map[string]string // only for non-GET; GET needs no request state
	AmountMicro int64
	Reason      string
	At          time.Time
}

// blockedStore remembers refused payments. Every method is safe for concurrent
// use. now is injectable so tests can advance the clock.
type blockedStore struct {
	mu       sync.Mutex
	items    []BlockedRequest
	nextID   uint64
	lastNote time.Time
	now      func() time.Time
}

func newBlockedStore(now func() time.Time) *blockedStore {
	if now == nil {
		now = time.Now
	}
	return &blockedStore{now: now}
}

// Record stores a refused request and reports whether a notification is due
// (first refusal inside the coalescing window). Method+URL+Body are always
// kept; headers only for methods that can carry a request body.
func (b *blockedStore) Record(method, rawURL string, body []byte, headers map[string]string, amountMicro int64, reason string) (BlockedRequest, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked()
	b.nextID++
	br := BlockedRequest{
		ID:          "b" + itoa(b.nextID),
		Method:      method,
		URL:         rawURL,
		Body:        append([]byte(nil), body...),
		AmountMicro: amountMicro,
		Reason:      reason,
		At:          b.now(),
	}
	if method != "GET" && method != "HEAD" {
		br.Headers = cloneHeaders(headers)
	}
	b.items = append(b.items, br)
	if len(b.items) > blockedMax {
		b.items = b.items[len(b.items)-blockedMax:]
	}
	notify := b.now().Sub(b.lastNote) >= blockedNotifyWindow
	if notify {
		b.lastNote = b.now()
	}
	return br, notify
}

// BlockedSummary is the shape exposed by /status: enough to show the owner what
// waits, with NO body and NO headers. The full request stays server-side and is
// only used to replay an approved payment — a blocked POST may carry the agent's
// credentials in a header, and /status is readable by any same-user process.
type BlockedSummary struct {
	ID          string `json:"id"`
	Method      string `json:"method"`
	URL         string `json:"url"`
	AmountMicro int64  `json:"amount_micro"`
	Reason      string `json:"reason"`
	At          string `json:"at"`
}

// Summaries returns the safe view of the live entries, oldest first.
func (b *blockedStore) Summaries() []BlockedSummary {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked()
	out := make([]BlockedSummary, 0, len(b.items))
	for _, it := range b.items {
		out = append(out, BlockedSummary{
			ID:          it.ID,
			Method:      it.Method,
			URL:         it.URL,
			AmountMicro: it.AmountMicro,
			Reason:      it.Reason,
			At:          it.At.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// List returns a copy of the live entries, oldest first.
func (b *blockedStore) List() []BlockedRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked()
	out := make([]BlockedRequest, len(b.items))
	copy(out, b.items)
	return out
}

// Get returns one entry by id.
func (b *blockedStore) Get(id string) (BlockedRequest, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked()
	for _, it := range b.items {
		if it.ID == id {
			return it, true
		}
	}
	return BlockedRequest{}, false
}

// Remove drops an entry (after the owner acted on it).
func (b *blockedStore) Remove(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, it := range b.items {
		if it.ID == id {
			b.items = append(b.items[:i], b.items[i+1:]...)
			return true
		}
	}
	return false
}

func (b *blockedStore) sweepLocked() {
	cutoff := b.now().Add(-blockedTTL)
	kept := b.items[:0]
	for _, it := range b.items {
		if it.At.After(cutoff) {
			kept = append(kept, it)
		}
	}
	b.items = kept
}

// recordIfBlocked stores a refused payment the owner can act on: a CDP MFA
// request, or an overridable policy denial (budget, per-seller, unknown seller,
// price change). Other errors are not "needs the owner" and are not listed.
func (s *Server) recordIfBlocked(method, rawURL string, body []byte, headers map[string]string, err error) {
	var perr *gateway.PolicyError
	if !errors.As(err, &perr) {
		return
	}
	if perr.Code != "mfa_required" && !perr.CanOverride {
		return
	}
	s.recordBlocked(method, rawURL, body, headers, perr.AmountMicro, perr.Code)
}

// recordBlocked is the handler-side entry point: store the refused request and
// notify the owner when the coalescing window allows it.
func (s *Server) recordBlocked(method, rawURL string, body []byte, headers map[string]string, amountMicro int64, reason string) {
	if s.blocked == nil {
		return
	}
	_, notify := s.blocked.Record(method, rawURL, body, headers, amountMicro, reason)
	if notify {
		gateway.Notify("x402 Gateway — payment needs you", "An agent payment was held: "+reason+". Open the panel to review.")
	}
}

func cloneHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// itoa is a tiny base-10 formatter so this file pulls no strconv for one call.
func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
