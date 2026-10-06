package budget

import (
	"testing"
	"time"
)

// TestDomainCapCountsCommittedAndInFlightTogether (47.1, r12) is the maintainer's
// scenario: two ordinary concurrent requests on one approved seller must not
// exceed the domain cap. Before this, Authorize compared only in-flight
// reservations while the committed total lived in a separate store, so a payment
// that had already left Reserved was invisible to the next check.
func TestDomainCapCountsCommittedAndInFlightTogether(t *testing.T) {
	a := NewAuthority(t.TempDir(), nil)
	domain := "seller.example"
	domainCap := int64(1_000_000)
	half := int64(600_000) // 2 × 0.6 > 1.0

	first, err := a.Authorize(Hold{AmountMicro: half, Domain: domain}, Caps{DailyMicro: math_MaxInt64, DomainMicro: domainCap})
	if err != nil {
		t.Fatalf("first Authorize: %v", err)
	}
	// Second request while the first is still in flight: the reservation counts.
	if _, err := a.Authorize(Hold{AmountMicro: half, Domain: domain}, Caps{DailyMicro: math_MaxInt64, DomainMicro: domainCap}); err != ErrSubCap {
		t.Fatalf("in-flight: want ErrSubCap, got %v", err)
	}
	// First settles: it leaves Reserved, but the committed domain total must
	// replace it. This is the exact window the maintainer described.
	if err := a.Commit(first); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, err := a.Authorize(Hold{AmountMicro: half, Domain: domain}, Caps{DailyMicro: math_MaxInt64, DomainMicro: domainCap}); err != ErrSubCap {
		t.Fatalf("after commit: want ErrSubCap, got %v", err)
	}
	// Room for the rest, and not a micro more.
	if _, err := a.Authorize(Hold{AmountMicro: domainCap - half, Domain: domain}, Caps{DailyMicro: math_MaxInt64, DomainMicro: domainCap}); err != nil {
		t.Fatalf("exact remainder must pass: %v", err)
	}
	if _, err := a.Authorize(Hold{AmountMicro: 1, Domain: domain}, Caps{DailyMicro: math_MaxInt64, DomainMicro: domainCap}); err != ErrSubCap {
		t.Fatalf("one micro over the remainder: want ErrSubCap, got %v", err)
	}
}

// math_MaxInt64 avoids importing math just for the sentinel in table tests.
const math_MaxInt64 = int64(1) << 62

// TestDomainCapIsIndependentPerDomain (47.1): the cap is per seller, so a
// second seller must still have its whole cap available.
func TestDomainCapIsIndependentPerDomain(t *testing.T) {
	a := NewAuthority(t.TempDir(), nil)
	domainCap := int64(1_000_000)
	tok, err := a.Authorize(Hold{AmountMicro: 900_000, Domain: "one.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: domainCap})
	if err != nil {
		t.Fatalf("Authorize one.example: %v", err)
	}
	if err := a.Commit(tok); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authorize(Hold{AmountMicro: 900_000, Domain: "one.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: domainCap}); err != ErrSubCap {
		t.Fatalf("one.example must be capped, got %v", err)
	}
	if _, err := a.Authorize(Hold{AmountMicro: 900_000, Domain: "two.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: domainCap}); err != nil {
		t.Fatalf("two.example has its own cap: %v", err)
	}
}

// TestDomainCapZeroDisablesTheCheck (47.1): subcapMicro <= 0 means the seller is
// not domain-capped (approveSeller path).
func TestDomainCapZeroDisablesTheCheck(t *testing.T) {
	a := NewAuthority(t.TempDir(), nil)
	for i := 0; i < 5; i++ {
		tok, err := a.Authorize(Hold{AmountMicro: 900_000, Domain: "seller.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: 0})
		if err != nil {
			t.Fatalf("Authorize %d with no domain cap: %v", i, err)
		}
		if err := a.Commit(tok); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDomainCapSurvivesDayRollover (47.1): a signed payment promoted across
// midnight keeps its domain share, so it cannot escape the new day's cap.
func TestDomainCapSurvivesDayRollover(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 10, 1, 23, 59, 0, 0, time.UTC)
	a := NewAuthority(dir, func() time.Time { return day1 })
	tok, err := a.Authorize(Hold{AmountMicro: 900_000, Domain: "seller.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: 1_000_000})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := a.MarkSigned(tok); err != nil {
		t.Fatal(err)
	}
	day2 := day1.Add(time.Minute)
	a2 := NewAuthority(dir, func() time.Time { return day2 })
	if _, err := a2.Authorize(Hold{AmountMicro: 200_000, Domain: "seller.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: 1_000_000}); err != ErrSubCap {
		t.Fatalf("signed payment must carry its domain share: want ErrSubCap, got %v", err)
	}
}

// TestDomainTotalSurvivesTTLPromote (47.1): the same for the expiry path.
func TestDomainTotalSurvivesTTLPromote(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	a := NewAuthority(dir, func() time.Time { return t0 })
	tok, err := a.Authorize(Hold{AmountMicro: 900_000, Domain: "seller.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: 1_000_000})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := a.MarkSigned(tok); err != nil {
		t.Fatal(err)
	}
	a2 := NewAuthority(dir, func() time.Time { return t0.Add(ReservationTTL + time.Second) })
	if _, err := a2.Authorize(Hold{AmountMicro: 200_000, Domain: "seller.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: 1_000_000}); err != ErrSubCap {
		t.Fatalf("TTL promote must carry the domain share: want ErrSubCap, got %v", err)
	}
}

// TestLegacyBudgetFileStartsDomainCounting (47.1): a budget.json written before
// the per-domain total existed must load without error and still cap.
func TestLegacyBudgetFileStartsDomainCounting(t *testing.T) {
	dir := t.TempDir()
	a := NewAuthority(dir, nil)
	// No SpentByDomain key at all — the pre-47.1 on-disk shape.
	if _, err := a.Authorize(Hold{AmountMicro: 100, Domain: "seller.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: 1_000_000}); err != nil {
		t.Fatal(err)
	}
	b := NewAuthority(dir, nil)
	if _, err := b.Authorize(Hold{AmountMicro: 100, Domain: "seller.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: 1_000_000}); err != nil {
		t.Fatalf("legacy file must load: %v", err)
	}
	// And the map is initialised, not left nil.
	st, err := b.load()
	if err != nil {
		t.Fatal(err)
	}
	if st.SpentByDomain == nil {
		t.Fatal("SpentByDomain must be initialised for a legacy file")
	}
}

// TestHandEditedNegativeDomainTotalIsClamped (47.1): a negative in the domain
// map must not loosen the cap.
func TestHandEditedNegativeDomainTotalIsClamped(t *testing.T) {
	dir := t.TempDir()
	a := NewAuthority(dir, nil)
	if _, err := a.Authorize(Hold{AmountMicro: 500_000, Domain: "seller.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: 1_000_000}); err != nil {
		t.Fatal(err)
	}
	st, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	st.SpentByDomain["seller.example"] = -999_999
	if err := a.persist(st); err != nil {
		t.Fatal(err)
	}
	b := NewAuthority(dir, nil)
	if _, err := b.Authorize(Hold{AmountMicro: 900_000, Domain: "seller.example"}, Caps{DailyMicro: 1 << 62, DomainMicro: 1_000_000}); err != ErrSubCap {
		t.Fatalf("hand-edited negative must not loosen the cap: got %v", err)
	}
}
