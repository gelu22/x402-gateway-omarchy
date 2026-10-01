package gateway

import (
	"gateway/internal/spend"
	"math"
	"testing"
)

// FuzzSpendAdd validates monotonicity and saturation of spend.Tracker.Add —
// amounts never wrap to negative or below their pre-add value (016.4, 016.6b).
// The SellerRegistry half moved to internal/budget with SpentByDomain (47.1),
// where the per-domain total is decided; the saturation property is pinned by
// TestHandEditedNegativeDomainTotalIsClamped there.
func FuzzSpendAdd(f *testing.F) {
	f.Add(int64(1))
	f.Add(int64(1000000))
	f.Add(int64(math.MaxInt64 - 1))
	f.Add(int64(math.MaxInt64))
	f.Add(int64(0))

	f.Fuzz(func(t *testing.T, amount int64) {
		if amount < 0 {
			return // negative amounts rejected by both Adders
		}

		// --- spend.Tracker ---
		dir, _ := t.TempDir(), true
		if dir == "" {
			t.Skip("temp dir creation failed")
		}
		tr := spend.NewTracker(dir)

		// First Add.
		err := tr.Add(amount)
		if amount <= 0 {
			if err == nil {
				t.Fatalf("Add(0) returned nil error")
			}
			return
		}
		if err != nil {
			t.Fatalf("Add(%d) failed: %v", amount, err)
		}

		today, err := tr.Today()
		if err != nil {
			t.Fatalf("Today after Add(%d): %v", amount, err)
		}
		if today != amount {
			t.Fatalf("Today after first Add(%d) = %d, want %d", amount, today, amount)
		}

		// Second Add — monotonicity: must never decrease.
		if err := tr.Add(amount); err != nil {
			t.Fatalf("Add(%d) second time failed: %v", amount, err)
		}
		today2, err := tr.Today()
		if err != nil {
			t.Fatalf("Today after second Add(%d): %v", amount, err)
		}
		if today2 < today {
			t.Fatalf("Monotonicity violated: Today after Add(%d) went from %d to %d", amount, today, today2)
		}
		// Saturate check: adding MaxInt64 to MaxInt64 must stay at MaxInt64,
		// never wrap to a negative number.
		if amount == math.MaxInt64 {
			if today2 != math.MaxInt64 {
				t.Fatalf("Spend saturate failed: expected MaxInt64, got %d", today2)
			}
			// Third add to MaxInt64 — should still be MaxInt64 (saturated).
			tr.Add(amount)
			today3, _ := tr.Today()
			if today3 != math.MaxInt64 {
				t.Fatalf("Spend re-saturate failed: expected MaxInt64 after 3x MaxInt64, got %d", today3)
			}
		}

	})
}
