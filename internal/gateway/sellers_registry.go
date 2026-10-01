// Seller registry persistence: load, save, Known, Today, Land, Add.
package gateway

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

func (r *SellerRegistry) load() (sellerState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadLocked()
}

func (r *SellerRegistry) loadLocked() (sellerState, error) {
	today := r.today()
	st := sellerState{Day: today, Domains: map[string]*sellerEntry{}}
	raw, err := os.ReadFile(r.path())
	if os.IsNotExist(err) {
		return mergeSellerMem(st, r.mem, today), nil
	}
	if err != nil {
		return st, fmt.Errorf("sellers: read: %w", err)
	}
	var disk sellerState
	if err := json.Unmarshal(raw, &disk); err != nil {
		return mergeSellerMem(st, r.mem, today), nil
	}
	for domain, e := range disk.Domains {
		if e == nil {
			continue
		}
		if e.Day != today {
			if e.Day > today && isSellerDay(e.Day) {
				e = &sellerEntry{FirstSeen: e.FirstSeen, Day: today, DaySpendMicro: e.DaySpendMicro}
			} else {
				e = &sellerEntry{FirstSeen: e.FirstSeen, Day: today}
			}
		}
		if e.DaySpendMicro < 0 {
			e.DaySpendMicro = 0
		}
		if key := normKey(domain); key != "" {
			if prev, ok := st.Domains[key]; ok {
				if prev.DaySpendMicro > math.MaxInt64-e.DaySpendMicro {
					prev.DaySpendMicro = math.MaxInt64
				} else {
					prev.DaySpendMicro += e.DaySpendMicro
				}
				if e.FirstSeen < prev.FirstSeen {
					prev.FirstSeen = e.FirstSeen
				}
			} else {
				st.Domains[key] = e
			}
		}
	}
	return mergeSellerMem(st, r.mem, today), nil
}

func isSellerDay(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for _, c := range []byte(s) {
		if (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func (r *SellerRegistry) saveLocked(st sellerState) error {
	st.Day = r.today()
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := r.path() + ".tmp"
	if err := os.WriteFile(tmp, raw, sellersFilePerms); err != nil {
		return fmt.Errorf("sellers: write: %w", err)
	}
	return os.Rename(tmp, r.path())
}

func (r *SellerRegistry) Known(domain string) (bool, error) {
	domain = normKey(domain)
	if domain == "" {
		return false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := r.loadLocked()
	if err != nil {
		return false, err
	}
	_, ok := st.Domains[domain]
	return ok, nil
}

func (r *SellerRegistry) Land(domain string) error {
	domain = normKey(domain)
	if domain == "" {
		return fmt.Errorf("sellers: refusing to land empty domain")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	st, err := r.loadLocked()
	if err != nil {
		return err
	}
	if _, ok := st.Domains[domain]; !ok {
		st.Domains[domain] = &sellerEntry{FirstSeen: r.today(), Day: r.today()}
	}
	// Persist first: a failed Land must not leave Known dirty (TOFU / 45.4).
	if err := r.saveLocked(st); err != nil {
		return err
	}
	r.mem = cloneSellerState(st)
	return nil
}
