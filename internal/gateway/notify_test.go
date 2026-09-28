//go:build linux

package gateway

import (
	"slices"
	"testing"
)

// TestNotifyArgsIsTransient pins the argv contract: the budget-exhausted toast
// must use a "normal" urgency and an explicit TTL, never a persistent toast.
func TestNotifyArgsIsTransient(t *testing.T) {
	want := []string{"notify-send", "-a", "x402 Gateway", "-u", "normal", "-t", "10000", "t", "b"}
	got := notifyArgs("t", "b")
	if !slices.Equal(got, want) {
		t.Fatalf("notifyArgs = %q, want %q", got, want)
	}
	for _, a := range got {
		if a == "critical" || a == "-1" {
			t.Fatalf("notifyArgs contains non-expiring flag %q", a)
		}
	}
}
