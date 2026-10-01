package budget

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

func (a *Authority) path() string { return filepath.Join(a.stateDir, "budget.json") }

// load reads today's state, sweeping expired reservations and rolling over
// on day change.
//
// Corrupt file → fresh day: Spent starts at 0 and the previous total is LOST
// without an error. The cap is still enforced from zero, so this is a silent
// reset rather than a cap bypass. Known residual, deliberately open (44.2
// "SKIP / RESIDUAL"); the window covers replacement, not just corruption.
//
// TTL: unsigned expired → drop; signed expired → promote to Spent (persist).
// Day rollover (44.4b): yesterday's committed Spent resets; signed Reserved →
// Spent on the new day (fail-closed carry); unsigned Reserved carries over as
// still-unsigned with a renewed TTL when it has NOT expired, and drops when it
// has (46.6 — dropping a live unsigned hold made MarkSigned a silent no-op, so
// a payment straddling midnight left the daily cap uncounted).
func (a *Authority) load() (state, error) {
	var st state
	raw, err := os.ReadFile(a.path())
	if os.IsNotExist(err) {
		return state{Day: a.today(), Reserved: map[string]reservation{}}, nil
	}
	if err != nil {
		return st, fmt.Errorf("budget: read: %w", err)
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return state{Day: a.today(), Reserved: map[string]reservation{}}, nil
	}
	today := a.today()
	dirty := false
	if st.Day != today {
		carry := int64(0)
		// Unsigned holds that have not expired must survive the rollover: the
		// caller still holds the token and will call MarkSigned. Dropping them
		// made MarkSigned a silent no-op (unknown token), so a payment
		// straddling midnight went out unaccounted (46.6, F1).
		fresh := make(map[string]reservation, len(st.Reserved))
		for k, r := range st.Reserved {
			if r.Signed {
				carry = satAddSpent(carry, r.AmountMicro)
				continue
			}
			if a.now().After(r.ExpiresAt) {
				continue // expired unsigned hold: drop, as before
			}
			r.ExpiresAt = a.now().Add(ReservationTTL)
			fresh[k] = r // stays UNSIGNED: no signature, no charge (44.4b)
		}
		st = state{Day: today, Spent: carry, Reserved: fresh}
		dirty = true
	}
	if st.Reserved == nil {
		st.Reserved = map[string]reservation{}
	}
	for k, r := range st.Reserved {
		if !a.now().After(r.ExpiresAt) {
			continue
		}
		if r.Signed {
			st.Spent = satAddSpent(st.Spent, r.AmountMicro)
		}
		delete(st.Reserved, k)
		dirty = true
	}
	// Hand-edited negatives must never loosen the budget.
	if st.Spent < 0 {
		st.Spent = 0
		dirty = true
	}
	if dirty {
		if err := a.persist(st); err != nil {
			return st, err // fail-closed: unreconciled ledger must not proceed
		}
	}
	return st, nil
}

// persist writes the state atomically (tmp+rename, 0600).
func (a *Authority) persist(st state) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := a.path() + ".tmp"
	if err := os.WriteFile(tmp, raw, filePerms); err != nil {
		return fmt.Errorf("budget: write: %w", err)
	}
	return os.Rename(tmp, a.path())
}

// reservedTotal returns the sum of all active reservation amounts.
func reservedTotal(st state) int64 {
	var sum int64
	for _, r := range st.Reserved {
		if sum > math.MaxInt64-r.AmountMicro {
			return math.MaxInt64
		}
		sum += r.AmountMicro
	}
	return sum
}

// reservedForDomain returns the sum of reservations for one domain.
func reservedForDomain(st state, domain string) int64 {
	var sum int64
	for _, r := range st.Reserved {
		if r.Domain != domain {
			continue
		}
		if sum > math.MaxInt64-r.AmountMicro {
			return math.MaxInt64
		}
		sum += r.AmountMicro
	}
	return sum
}
