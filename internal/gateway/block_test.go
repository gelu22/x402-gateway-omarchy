package gateway

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBlockPersistAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	bt := NewBlockTracker(dir, nil)
	bt.Record(BlockRecord{
		Reason: "budget_exceeded", AmountMicro: 8000, Domain: "api.example.com",
	})

	// "Restart": fresh tracker over the same state dir.
	bt2 := NewBlockTracker(dir, nil)
	got := bt2.Current()
	if got == nil || got.Reason != "budget_exceeded" {
		t.Fatalf("persisted block mismatch: %+v", got)
	}
}

func TestNotifyDedupOncePerDay(t *testing.T) {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	bt := NewBlockTracker(dir, fixedClock(base))

	if !bt.TryMarkNotified() {
		t.Fatal("first exhaustion must notify")
	}
	if bt.TryMarkNotified() {
		t.Fatal("second same-day notification must be deduped")
	}

	// Next day → notify again (the mark is persisted, so a restart sees it too).
	bt2 := NewBlockTracker(dir, fixedClock(base.Add(24*time.Hour)))
	if !bt2.TryMarkNotified() {
		t.Fatal("next day must notify again")
	}
}

// A retry burst used to fire one notification per blocked fetch: ShouldNotify
// and MarkNotified were separate calls with the send in between. The reserve
// must win exactly once no matter how many blocks land together.
func TestNotifyDedupUnderBurst(t *testing.T) {
	dir := t.TempDir()
	bt := NewBlockTracker(dir, fixedClock(time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)))

	const callers = 32
	var wg sync.WaitGroup
	var wins atomic.Int64
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if bt.TryMarkNotified() {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := wins.Load(); got != 1 {
		t.Fatalf("reservations = %d, want exactly 1", got)
	}
}

func TestClearOnSuccess(t *testing.T) {
	dir := t.TempDir()
	bt := NewBlockTracker(dir, nil)
	bt.Record(BlockRecord{Reason: "budget_exceeded"})
	if bt.Current() == nil {
		t.Fatal("record missing")
	}
	bt.Clear()
	if bt.Current() != nil {
		t.Fatal("clear failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "block.json")); err == nil {
		raw, _ := os.ReadFile(filepath.Join(dir, "block.json"))
		if len(raw) > 2 {
			t.Fatalf("block.json should be empty-ish after clear, got %s", raw)
		}
	}
}

func TestNonBudgetRecorded(t *testing.T) {
	dir := t.TempDir()
	bt := NewBlockTracker(dir, fixedClock(time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)))
	bt.Record(BlockRecord{Reason: "invalid_amount", AmountMicro: 999999})
	if bt.Current() == nil || bt.Current().Reason != "invalid_amount" {
		t.Fatal("invalid_amount should be recorded")
	}
}

func fixedClock(base time.Time) func() time.Time {
	return func() time.Time { return base }
}
