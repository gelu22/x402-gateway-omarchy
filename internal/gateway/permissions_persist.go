// permissions_persist.go — disk shape and validation for the permission store
// (split from permissions.go to stay under the 200-line budget, AGENTS rule 5).
package gateway

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (p *PermissionStore) path() string { return filepath.Join(p.stateDir, "permissions.json") }

type permissionFile struct {
	Permanent []Permission `json:"permanent"`
}

func (p *PermissionStore) loadLocked() {
	raw, err := os.ReadFile(p.path())
	if err != nil {
		return // missing or unreadable → no permissions (fail-closed)
	}
	var pf permissionFile
	if json.Unmarshal(raw, &pf) != nil {
		return // corrupt → no permissions (fail-closed)
	}
	for _, it := range pf.Permanent {
		if validPermissionURL(it.URL) && it.LimitMicro > 0 {
			p.permanent = append(p.permanent, Permission{URL: it.URL, LimitMicro: it.LimitMicro})
		}
	}
}

func (p *PermissionStore) saveLocked() error {
	raw, err := json.Marshal(permissionFile{Permanent: p.permanent})
	if err != nil {
		return err
	}
	tmp := p.path() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p.path())
}

func (p *PermissionStore) sweepLocked() {
	now := p.now()
	keep := p.temporary[:0]
	for _, it := range p.temporary {
		exp, err := time.Parse(time.RFC3339, it.ExpiresAt)
		if err != nil || now.Before(exp) {
			keep = append(keep, it)
		}
	}
	p.temporary = keep
}

// validPermissionURL accepts only http(s) URLs with a non-empty host (mirrors
// the panel's isValidRememberedUrl).
func validPermissionURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}
