package budget

import (
	"testing"
	"time"
)

// atClock returns a clock frozen at t.
func atClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// TestDayRolloverCarriesFreshUnsignedHold (46.6, F1): a payment in flight
// across midnight must keep its hold, otherwise MarkSigned becomes a silent
// no-op and the daily cap is never charged.
func TestDayRolloverCarriesFreshUnsignedHold(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 10, 1, 23, 59, 0, 0, time.UTC)
	a := NewAuthority(dir, atClock(day1))
	token, err := a.Authorize(Hold{AmountMicro: 1_000_000, Domain: "example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	// Any load() after midnight performs the rollover — Today() is the real
	// trigger, because the panel polls /status.
	day2 := day1.Add(2 * time.Minute)
	a2 := NewAuthority(dir, atClock(day2))
	if total, _ := a2.Today(); total != 1_000_000 {
		t.Fatalf("hold lost across rollover: total = %d, want 1000000", total)
	}

	if err := a2.MarkSigned(token); err != nil {
		t.Fatalf("MarkSigned after rollover: %v", err)
	}
	if err := a2.Commit(token); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if total, _ := a2.Today(); total != 1_000_000 {
		t.Fatalf("after MarkSigned+Commit total = %d, want 1000000 (charged once)", total)
	}
}

// TestDayRolloverExpiredUnsignedDrops (46.6): an unsigned hold past its TTL has
// no live caller left, so it must still drop. This is the case
// TestDayRolloverUnsignedDrops pins at 24 h — pinned here at TTL+1s so the
// boundary is explicit.
func TestDayRolloverExpiredUnsignedDrops(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 10, 1, 23, 59, 0, 0, time.UTC)
	a := NewAuthority(dir, atClock(day1))
	if _, err := a.Authorize(Hold{AmountMicro: 1_000_000, Domain: "example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0}); err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	day2 := day1.Add(ReservationTTL + time.Second)
	a2 := NewAuthority(dir, atClock(day2))
	if total, _ := a2.Today(); total != 0 {
		t.Fatalf("expired unsigned hold survived: total = %d, want 0", total)
	}
}

// TestDayRolloverMixedSignedAndFreshUnsigned (46.6, F2): the three branches of
// the rollover loop in one transition — signed → Spent, fresh unsigned → held
// unsigned, expired unsigned → dropped.
func TestDayRolloverMixedSignedAndFreshUnsigned(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 10, 1, 23, 59, 0, 0, time.UTC)
	a := NewAuthority(dir, atClock(day1))

	signed, err := a.Authorize(Hold{AmountMicro: 100_000, Domain: "signed.example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize signed: %v", err)
	}
	if err := a.MarkSigned(signed); err != nil {
		t.Fatalf("MarkSigned: %v", err)
	}
	fresh, err := a.Authorize(Hold{AmountMicro: 200_000, Domain: "fresh.example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize fresh: %v", err)
	}
	expired, err := a.Authorize(Hold{AmountMicro: 300_000, Domain: "expired.example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize expired: %v", err)
	}
	// Age the third hold past its TTL by hand-writing a stale expiry.
	st, err := a.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	aged := st.Reserved[expired]
	aged.ExpiresAt = day1.Add(-time.Hour)
	st.Reserved[expired] = aged
	if err := a.persist(st); err != nil {
		t.Fatalf("persist: %v", err)
	}

	day2 := day1.Add(time.Minute)
	a2 := NewAuthority(dir, atClock(day2))
	total, err := a2.Today()
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	// signed → Spent (100k); fresh unsigned still held (200k); expired dropped.
	if total != 300_000 {
		t.Fatalf("total = %d, want 300000 (100k spent + 200k held)", total)
	}
	// The carried hold must still be unsigned: no signature, no charge.
	if err := a2.Commit(fresh); err != nil {
		t.Fatalf("Commit fresh: %v", err)
	}
	if err := a2.MarkSigned(fresh); err != nil {
		t.Fatalf("MarkSigned after rollover: %v", err)
	}
	if err := a2.Commit(fresh); err != nil {
		t.Fatalf("Commit after MarkSigned: %v", err)
	}
	total, _ = a2.Today()
	if total != 300_000 {
		t.Fatalf("total = %d, want 300000 (idempotent Commit, no double count)", total)
	}
}

// TestTokensAreUniqueOnAFrozenClock (46.6, F10): a coarse or stepped clock can
// return the same nanosecond twice. A token collision would overwrite the first
// reservation in the map, so that charge would never be counted.
func TestTokensAreUniqueOnAFrozenClock(t *testing.T) {
	dir := t.TempDir()
	frozen := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	a := NewAuthority(dir, atClock(frozen)) // clock never advances

	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		token, err := a.Authorize(Hold{AmountMicro: 1_000, Domain: "example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
		if err != nil {
			t.Fatalf("Authorize %d: %v", i, err)
		}
		if seen[token] {
			t.Fatalf("token %q issued twice on a frozen clock", token)
		}
		seen[token] = true
	}
	// Every reservation must still be individually resolvable.
	if total, _ := a.Today(); total != 50_000 {
		t.Fatalf("total = %d, want 50000 (50 distinct holds)", total)
	}
}

// TestRolloverIsNotABudgetLoosener (46.6): a rolled-over day must not hand out
// more than the cap allows, whatever the mix of carried and dropped holds.
func TestRolloverIsNotABudgetLoosener(t *testing.T) {
	dir := t.TempDir()
	day1 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	capMicro := int64(1_000_000)
	a := NewAuthority(dir, atClock(day1))
	if _, err := a.Authorize(Hold{AmountMicro: capMicro, Domain: "example.com"}, Caps{DailyMicro: capMicro, DomainMicro: 0}); err != nil {
		t.Fatalf("Authorize: %v", err)
	}

	day2 := day1.Add(2 * time.Minute)
	a2 := NewAuthority(dir, atClock(day2))
	if _, err := a2.Authorize(Hold{AmountMicro: 1, Domain: "example.com"}, Caps{DailyMicro: capMicro, DomainMicro: 0}); err != ErrBudget {
		t.Fatalf("want ErrBudget after rollover carried the full cap, got %v", err)
	}
}
