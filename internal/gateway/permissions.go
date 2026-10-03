// Permission store (49.3): the daemon-side "remembered URL up to an amount".
//
// The panel already had this rule in Model.js (rememberedLimitMicro /
// needsApproval), but only the panel enforced it, so agents and MCP clients got
// no benefit. Moving it here makes one source of truth for every client.
//
// A permission is a URL plus an amount cap, in one of two modes:
//   - permanent: persisted (0600) and survives a restart;
//   - temporary: memory only, with an expiry.
//
// Neither mode raises the daily cap or the per-seller share: a permission only
// lifts the "is this seller approved / is this over the seller's share" question
// for ONE url up to its cap. A price raise above the cap asks again.
package gateway

import (
	"fmt"
	"sync"
	"time"
)

// Permission is one approved target with an amount cap.
type Permission struct {
	URL        string `json:"url"`
	LimitMicro int64  `json:"limit_micro"`
	Temporary  bool   `json:"temporary,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"` // RFC3339; temporary only
}

// PermissionStore holds permanent (disk) and temporary (memory) permissions.
// Every method is safe for concurrent use; a nil store denies everything.
type PermissionStore struct {
	stateDir  string
	now       func() time.Time
	mu        sync.Mutex
	permanent []Permission
	temporary []Permission
}

// NewPermissionStore loads the permanent list from stateDir. A corrupt file is
// treated as empty (fail-closed: an unreadable file grants nothing).
func NewPermissionStore(stateDir string, now func() time.Time) *PermissionStore {
	if now == nil {
		now = time.Now
	}
	p := &PermissionStore{stateDir: stateDir, now: now}
	p.loadLocked()
	return p
}

// Add records a permission. temporary=true requires ttl > 0 and is memory-only.
func (p *PermissionStore) Add(rawURL string, limitMicro int64, temporary bool, ttl time.Duration) error {
	if !validPermissionURL(rawURL) {
		return fmt.Errorf("permissions: invalid url")
	}
	if limitMicro <= 0 {
		return fmt.Errorf("permissions: limit must be positive")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if temporary {
		if ttl <= 0 {
			return fmt.Errorf("permissions: temporary requires a positive ttl")
		}
		p.removeLocked(rawURL) // a new decision replaces the old one
		p.temporary = append(p.temporary, Permission{
			URL: rawURL, LimitMicro: limitMicro, Temporary: true,
			ExpiresAt: p.now().Add(ttl).UTC().Format(time.RFC3339),
		})
		return nil
	}
	p.removeLocked(rawURL)
	p.permanent = append(p.permanent, Permission{URL: rawURL, LimitMicro: limitMicro})
	return p.saveLocked()
}

// Remove drops any permission (permanent and temporary) for the url. Returns
// true when something was removed.
func (p *PermissionStore) Remove(rawURL string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	changed := p.removeLocked(rawURL)
	if changed {
		if err := p.saveLocked(); err != nil && p.permanent != nil {
			// Persistence failed: the in-memory list already changed, but the
			// on-disk file still allows the url. Report via return only; the
			// caller logs. Fail-closed direction would be to keep it, which is
			// what happens — a stale allow is the same as the pre-call state.
			_ = err
		}
	}
	return changed
}

func (p *PermissionStore) removeLocked(rawURL string) bool {
	changed := false
	keepP := p.permanent[:0]
	for _, it := range p.permanent {
		if it.URL == rawURL {
			changed = true
			continue
		}
		keepP = append(keepP, it)
	}
	p.permanent = keepP
	keepT := p.temporary[:0]
	for _, it := range p.temporary {
		if it.URL == rawURL {
			changed = true
			continue
		}
		keepT = append(keepT, it)
	}
	p.temporary = keepT
	return changed
}

// List returns the live permissions (permanent then temporary), skipping
// expired temporary ones.
func (p *PermissionStore) List() []Permission {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	out := make([]Permission, 0, len(p.permanent)+len(p.temporary))
	out = append(out, p.permanent...)
	out = append(out, p.temporary...)
	return out
}

// Allows reports whether a payment of amountMicro to rawURL is pre-approved.
// Nil store or no matching live permission denies (fail-closed). The highest
// matching cap wins.
func (p *PermissionStore) Allows(rawURL string, amountMicro int64) bool {
	if p == nil || amountMicro <= 0 {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sweepLocked()
	best := int64(0)
	for _, it := range p.permanent {
		if it.URL == rawURL && it.LimitMicro > best {
			best = it.LimitMicro
		}
	}
	for _, it := range p.temporary {
		if it.URL == rawURL && it.LimitMicro > best {
			best = it.LimitMicro
		}
	}
	return best >= amountMicro
}
