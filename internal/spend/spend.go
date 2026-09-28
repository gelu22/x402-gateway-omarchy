// Package spend tracks the single daily paid amount in local state; resets at
// local midnight. Amounts are USDC micro-units (6 decimals). The PPV/PPC
// buckets were removed in 017.12 (one daily cap).
package spend

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const filePerms = 0o600

type Tracker struct {
	stateDir string
	now      func() time.Time
	// mu serializes Add's read-modify-write: concurrent paid fetches must
	// never lost-update (under-counted spend would loosen the budget).
	mu sync.Mutex
}

// state is the on-disk shape. Legacy PPV/PPC (and pre-bucket total) counters
// are summed into Micro once, so a bucket-era day does not silently reset.
type state struct {
	Day         string `json:"day"`
	Micro       int64  `json:"micro_usdc"`
	LegacyPPV   int64  `json:"ppv_micro_usdc,omitempty"`
	LegacyPPC   int64  `json:"ppc_micro_usdc,omitempty"`
	LegacyTotal int64  `json:"total_micro_usdc,omitempty"`
}

func NewTracker(stateDir string) *Tracker {
	return &Tracker{stateDir: stateDir, now: time.Now}
}

func (t *Tracker) path() string { return filepath.Join(t.stateDir, "spend.json") }

// load returns today's state, migrating legacy counters and rolling over on
// day change.
func (t *Tracker) load() (state, error) {
	var st state
	raw, err := os.ReadFile(t.path())
	if os.IsNotExist(err) {
		return state{Day: t.today()}, nil
	}
	if err != nil {
		return st, fmt.Errorf("spend: read: %w", err)
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return state{Day: t.today()}, nil // corrupt: fresh day (caps still enforced)
	}
	today := t.today()
	if st.Day != today {
		if st.Day > today && isCalendarDay(st.Day) {
			// Future-dated file (clock skew or tamper): keep counters, only
			// normalize the day. Resetting would silently wipe today's spend.
			// A malformed day is treated as past (reset path, never pinned).
			st.Day = today
		} else {
			st = state{Day: today}
		}
	}
	// Hand-edited negatives must never loosen the budget below zero.
	for _, v := range []*int64{&st.Micro, &st.LegacyPPV, &st.LegacyPPC, &st.LegacyTotal} {
		if *v < 0 {
			*v = 0
		}
	}
	if st.Micro == 0 {
		if sum := saturatingSum(st.LegacyPPV, st.LegacyPPC, st.LegacyTotal); sum > 0 {
			st.Micro = sum
		}
	}
	st.LegacyPPV, st.LegacyPPC, st.LegacyTotal = 0, 0, 0
	return st, nil
}

// saturatingSum adds non-negative values, clamping at MaxInt64 (a wrapped
// counter would under-count spend and loosen the budget).
func saturatingSum(vals ...int64) int64 {
	var sum int64
	for _, v := range vals {
		if sum > math.MaxInt64-v {
			return math.MaxInt64
		}
		sum += v
	}
	return sum
}

// Today returns the current daily total.
func (t *Tracker) Today() (int64, error) {
	st, err := t.load()
	if err != nil {
		return 0, err
	}
	return st.Micro, nil
}

// Add records a successful payment and persists atomically. Non-positive
// amounts are rejected: only the gateway's validated flow may move counters.
func (t *Tracker) Add(amountMicro int64) error {
	if amountMicro <= 0 {
		return fmt.Errorf("spend: refusing non-positive amount %d", amountMicro)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	st, err := t.load()
	if err != nil {
		return err
	}
	today := t.today()
	if st.Day != today {
		st = state{Day: today}
	}
	st.Day = today
	// Saturate instead of wrapping: a wrapped counter would under-count spend
	// and loosen the budget.
	if st.Micro > math.MaxInt64-amountMicro {
		st.Micro = math.MaxInt64
	} else {
		st.Micro += amountMicro
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := t.path() + ".tmp"
	if err := os.WriteFile(tmp, raw, filePerms); err != nil {
		return fmt.Errorf("spend: write: %w", err)
	}
	return os.Rename(tmp, t.path())
}

func (t *Tracker) today() string { return t.now().Format("2006-01-02") }

// isCalendarDay reports whether s looks like a zero-padded YYYY-MM-DD date,
// for which lexicographic order equals chronological order.
func isCalendarDay(s string) bool {
	if len(s) != len("2006-01-02") || s[4] != '-' || s[7] != '-' {
		return false
	}
	for _, c := range []byte(s) {
		if (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}
