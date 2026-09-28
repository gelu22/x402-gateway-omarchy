package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// BlockRecord describes the last denied payment (why the agent got nothing).
// Persisted so the reason survives daemon restarts — a silent budget
// exhaustion is unacceptable (owner requirement, Etap 3).
type BlockRecord struct {
	Reason      string `json:"reason"` // budget_exceeded | invalid_amount | unknown_seller | domain_cap_exceeded
	AmountMicro int64  `json:"amount_micro,omitempty"`
	Domain      string `json:"domain,omitempty"`
	Ts          string `json:"ts"`
}

type blockState struct {
	Last        *BlockRecord `json:"last_block,omitempty"`
	NotifiedDay string       `json:"notified_day,omitempty"`
}

// BlockTracker persists the last block and deduplicates the desktop
// notification: at most one per local day (one cap now, no buckets).
type BlockTracker struct {
	stateDir string
	now      func() time.Time

	mu          sync.Mutex
	last        *BlockRecord
	day         string
	notifiedDay string
}

func NewBlockTracker(stateDir string, now func() time.Time) *BlockTracker {
	if now == nil {
		now = time.Now
	}
	bt := &BlockTracker{stateDir: stateDir, now: now}
	raw, err := os.ReadFile(bt.path())
	if err == nil {
		var st blockState
		if json.Unmarshal(raw, &st) == nil {
			if st.Last != nil {
				bt.last = st.Last
			}
			bt.notifiedDay = st.NotifiedDay
			bt.day = now().Format("2006-01-02")
		}
	}
	return bt
}

func (bt *BlockTracker) path() string { return filepath.Join(bt.stateDir, "block.json") }

// Record stores the block reason durably.
func (bt *BlockTracker) Record(b BlockRecord) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if b.Ts == "" {
		b.Ts = bt.now().Format(time.RFC3339)
	}
	bt.last = &b
	bt.persistLocked()
}

// Current returns the last block record or nil.
func (bt *BlockTracker) Current() *BlockRecord {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	return bt.last
}

// Clear removes the block record (called after a successful payment or day
// rollover with fresh budget).
func (bt *BlockTracker) Clear() {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.last = nil
	bt.persistLocked()
}

// TryMarkNotified atomically reserves today's notification slot: it returns
// true for the first caller of the day and false for every later one, and it
// persists the mark before returning.
//
// Reserving *before* sending is what makes "at most one per local day" true
// under a burst: the previous ShouldNotify-then-MarkNotified pair was a
// check-then-act race, so N blocked fetches arriving together each saw "due"
// and each fired a notification (bug: notification storm during retries and
// tests).
func (bt *BlockTracker) TryMarkNotified() bool {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	if bt.notifiedDay == bt.today() {
		return false
	}
	bt.notifiedDay = bt.today()
	bt.persistLocked()
	return true
}

func (bt *BlockTracker) persistLocked() {
	st := blockState{Last: bt.last, NotifiedDay: bt.notifiedDay}
	raw, err := json.Marshal(st)
	if err != nil {
		return
	}
	tmp := bt.path() + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, bt.path())
	}
}

func (bt *BlockTracker) today() string { return bt.now().Format("2006-01-02") }
