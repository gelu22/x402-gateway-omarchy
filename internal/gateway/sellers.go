// Seller trust registry types and key helpers (013.2, TOFU).
package gateway

import (
	"net/url"

	"path/filepath"
	"strings"
	"sync"
	"time"
)

const sellersFilePerms = 0o600

type sellerEntry struct {
	FirstSeen     string `json:"firstSeen"`
	Day           string `json:"day"`
	DaySpendMicro int64  `json:"daySpendMicro"`
}

type sellerState struct {
	Day     string                  `json:"day"`
	Domains map[string]*sellerEntry `json:"domains"`
}

// SellerRegistry is the seller trust store. Zero value is not usable;
// construct with NewSellerRegistry. All methods are goroutine-safe.
type SellerRegistry struct {
	stateDir string
	now      func() time.Time
	mu       sync.Mutex
}

// NewSellerRegistry wires the registry to the daemon state dir.
func NewSellerRegistry(stateDir string) *SellerRegistry {
	return &SellerRegistry{stateDir: stateDir, now: time.Now}
}

func (r *SellerRegistry) path() string  { return filepath.Join(r.stateDir, "sellers.json") }
func (r *SellerRegistry) today() string { return r.now().Format("2006-01-02") }

// normSellerDomain reduces a request target to its registry key.
func normSellerDomain(rawTarget string) string {
	u, err := url.Parse(rawTarget)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func normKey(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}
