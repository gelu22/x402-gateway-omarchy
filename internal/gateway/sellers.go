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
	// mem holds the last in-process view after Add/Land. Survives a failed
	// persist so Today never under-counts (NEW-P1-1 / 44.7.1 fail-closed).
	mem *sellerState
}

// NewSellerRegistry wires the registry to the daemon state dir.
func NewSellerRegistry(stateDir string) *SellerRegistry {
	return &SellerRegistry{stateDir: stateDir, now: time.Now}
}

func (r *SellerRegistry) path() string  { return filepath.Join(r.stateDir, "sellers.json") }
func (r *SellerRegistry) today() string { return r.now().Format("2006-01-02") }

// normSellerDomain reduces a request target to its registry key.
//
// The key is a HOSTNAME, not a registrable domain: a.example.com and
// b.example.com are separate buckets, each with its own sub-cap. Folding to the
// registrable domain needs a public-suffix list (golang.org/x/net/publicsuffix),
// a new dependency, so it is deliberately not done — see the T1 boundary row in
// THREAT-MODEL and TestSubCapIsPerHostnameNotRegistrableDomain in sellers_test.go,
// which pins this behaviour so a future fold is a conscious decision.
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

func cloneSellerState(st sellerState) *sellerState {
	out := &sellerState{Day: st.Day, Domains: make(map[string]*sellerEntry, len(st.Domains))}
	for k, e := range st.Domains {
		if e == nil {
			continue
		}
		cp := *e
		out.Domains[k] = &cp
	}
	return out
}

// mergeSellerMem overlays same-day in-memory DaySpendMicro onto disk so a
// failed save after Add cannot loosen the domain sub-cap (max, never min).
func mergeSellerMem(disk sellerState, mem *sellerState, today string) sellerState {
	if mem == nil || len(mem.Domains) == 0 {
		return disk
	}
	st := *cloneSellerState(disk)
	if st.Domains == nil {
		st.Domains = map[string]*sellerEntry{}
	}
	st.Day = today
	for k, me := range mem.Domains {
		if me == nil || me.Day != today {
			continue
		}
		de, ok := st.Domains[k]
		if !ok {
			cp := *me
			st.Domains[k] = &cp
			continue
		}
		if me.DaySpendMicro > de.DaySpendMicro {
			de.DaySpendMicro = me.DaySpendMicro
		}
		if me.FirstSeen != "" && (de.FirstSeen == "" || me.FirstSeen < de.FirstSeen) {
			de.FirstSeen = me.FirstSeen
		}
	}
	return st
}
