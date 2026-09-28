package gateway

import (
	"testing"

	"gateway/internal/spend"
	"gateway/internal/telemetry"
)

func TestSpendTrackerAddAndToday(t *testing.T) {
	stateDir := t.TempDir()
	tr := spend.NewTracker(stateDir)

	if v, err := tr.Today(); err != nil || v != 0 {
		t.Fatalf("initial = %d, want 0", v)
	}
	if err := tr.Add(1_000_000); err != nil {
		t.Fatal(err)
	}
	if v, _ := tr.Today(); v != 1_000_000 {
		t.Fatalf("after add = %d, want 1000000", v)
	}
	if err := tr.Add(2000); err != nil {
		t.Fatal(err)
	}
	if v, _ := tr.Today(); v != 1_002_000 {
		t.Fatalf("after second add = %d, want 1002000", v)
	}

	// Persistence across restart.
	tr2 := spend.NewTracker(stateDir)
	if v, _ := tr2.Today(); v != 1_002_000 {
		t.Fatalf("after restart = %d, want 1002000", v)
	}
}

func TestTelemetryEnqueue(t *testing.T) {
	tel := telemetry.New("", t.TempDir())
	if tel == nil {
		t.Fatal("telemetry.New should not return nil")
	}
	tel.Enqueue(telemetry.Event{
		Type:        "payment",
		Domain:      "example.com",
		AmountMicro: 2000,
	})
	if !tel.Enabled() {
		t.Log("telemetry disabled (no backend URL) — enqueue is no-op")
	}
}
