package budget

import (
	"testing"
	"time"
)

// TestTTLSignedPromotesToSpent: MarkSigned then TTL → amount in Spent/Today
// (never forgotten — HANCORE b).
func TestTTLSignedPromotesToSpent(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := NewAuthority(dir, func() time.Time { return now })
	token, err := a.Authorize(Hold{AmountMicro: 1_000_000, Domain: "example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := a.MarkSigned(token); err != nil {
		t.Fatalf("MarkSigned: %v", err)
	}
	a2 := NewAuthority(dir, func() time.Time { return now.Add(ReservationTTL + time.Minute) })
	total, err := a2.Today()
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	if total != 1_000_000 {
		t.Fatalf("want 1000000 after signed TTL promote, got %d", total)
	}
	// Second load must not double-count (persist after promote).
	total2, _ := a2.Today()
	if total2 != 1_000_000 {
		t.Fatalf("want stable 1000000 on reload, got %d", total2)
	}
}

// TestMarkSignedUnknownNoop: Commit then MarkSigned / unknown token = no-op.
func TestMarkSignedUnknownNoop(t *testing.T) {
	a, _ := newTestAuthority(t)
	if err := a.MarkSigned("nonexistent"); err != nil {
		t.Fatalf("MarkSigned(unknown): %v", err)
	}
	token, err := a.Authorize(Hold{AmountMicro: 500_000, Domain: "example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := a.Commit(token); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := a.MarkSigned(token); err != nil {
		t.Fatalf("MarkSigned after Commit: %v", err)
	}
	total, _ := a.Today()
	if total != 500_000 {
		t.Fatalf("want 500000 (no double), got %d", total)
	}
}

// TestMarkSignedIdempotent: second MarkSigned keeps Signed and renews TTL.
func TestMarkSignedIdempotent(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	clock := now
	a := NewAuthority(dir, func() time.Time { return clock })
	token, _ := a.Authorize(Hold{AmountMicro: 1_000_000, Domain: "example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err := a.MarkSigned(token); err != nil {
		t.Fatalf("MarkSigned: %v", err)
	}
	clock = now.Add(ReservationTTL / 2)
	if err := a.MarkSigned(token); err != nil {
		t.Fatalf("MarkSigned again: %v", err)
	}
	// Still within renewed TTL from second MarkSigned.
	clock = now.Add(ReservationTTL/2 + ReservationTTL - time.Minute)
	total, _ := a.Today()
	if total != 1_000_000 {
		t.Fatalf("want reservation held after renew, got %d", total)
	}
}

// TestDayRolloverSignedPromotesToSpent: MarkSigned then +24h → charge survives
// as Spent on the new day (must NOT wipe — 44.4b closes 44.4 residual).
func TestDayRolloverSignedPromotesToSpent(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := NewAuthority(dir, func() time.Time { return now })
	token, err := a.Authorize(Hold{AmountMicro: 1_000_000, Domain: "example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := a.MarkSigned(token); err != nil {
		t.Fatalf("MarkSigned: %v", err)
	}
	a2 := NewAuthority(dir, func() time.Time { return now.Add(24 * time.Hour) })
	total, err := a2.Today()
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	if total != 1_000_000 {
		t.Fatalf("want 1000000 after day rollover signed carry, got %d", total)
	}
	// Stable across reload (no double promote).
	total2, _ := a2.Today()
	if total2 != 1_000_000 {
		t.Fatalf("want stable 1000000, got %d", total2)
	}
}

// TestDayRolloverUnsignedDrops: unsigned hold does not survive midnight.
// TestDayRolloverUnsignedDrops pins the EXPIRED case: the clock jumps 24 h,
// far beyond ReservationTTL (15 min), so the hold is stale and no caller can
// still sign it — it must drop. A hold that is still fresh at rollover is a
// different case and carries over instead: see 46.6
// (TestDayRolloverCarriesFreshUnsignedHold).
func TestDayRolloverUnsignedDrops(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := NewAuthority(dir, func() time.Time { return now })
	if _, err := a.Authorize(Hold{AmountMicro: 1_000_000, Domain: "example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0}); err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	a2 := NewAuthority(dir, func() time.Time { return now.Add(24 * time.Hour) })
	total, _ := a2.Today()
	if total != 0 {
		t.Fatalf("want 0 after unsigned day rollover, got %d", total)
	}
}

// TestDayRolloverMixedSignedAndCommitted: committed yesterday resets; signed carries.
func TestDayRolloverMixedSignedAndCommitted(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := NewAuthority(dir, func() time.Time { return now })
	tokPaid, err := a.Authorize(Hold{AmountMicro: 2_000_000, Domain: "a.com"}, Caps{DailyMicro: 10_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize paid: %v", err)
	}
	if err := a.Commit(tokPaid); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	tokSig, err := a.Authorize(Hold{AmountMicro: 3_000_000, Domain: "b.com"}, Caps{DailyMicro: 10_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize signed: %v", err)
	}
	if err := a.MarkSigned(tokSig); err != nil {
		t.Fatalf("MarkSigned: %v", err)
	}
	// Before rollover: 2M spent + 3M reserved = 5M
	before, _ := a.Today()
	if before != 5_000_000 {
		t.Fatalf("want 5000000 before rollover, got %d", before)
	}
	a2 := NewAuthority(dir, func() time.Time { return now.Add(24 * time.Hour) })
	after, _ := a2.Today()
	// Committed 2M resets; signed 3M carries → 3M
	if after != 3_000_000 {
		t.Fatalf("want 3000000 (signed carry only), got %d", after)
	}
}

// --- 44.5 ratchet names (map 1:1 to HANCORE b / 44.1) ---------------------

// TestTTLSignedReservationPromotesToSpent — HANCORE (b) ratchet name.
func TestTTLSignedReservationPromotesToSpent(t *testing.T) {
	TestTTLSignedPromotesToSpent(t)
}

// TestTTLUnsignedReservationDrops — unsigned TTL drop ratchet name.
func TestTTLUnsignedReservationDrops(t *testing.T) {
	TestTTLUnsignedDrops(t)
}

// TestMarkSignedThenCrashBeforeCommit_TTLKeepsCharge simulates crash after
// MarkSigned (sig may have left) without Commit: after TTL the charge is Spent.
func TestMarkSignedThenCrashBeforeCommit_TTLKeepsCharge(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := NewAuthority(dir, func() time.Time { return now })
	token, err := a.Authorize(Hold{AmountMicro: 750_000, Domain: "seller.example"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := a.MarkSigned(token); err != nil {
		t.Fatalf("MarkSigned: %v", err)
	}
	// Crash: drop Authority; no Commit. Reload with clock past TTL.
	a2 := NewAuthority(dir, func() time.Time { return now.Add(ReservationTTL + time.Minute) })
	total, err := a2.Today()
	if err != nil {
		t.Fatalf("Today after crash+TTL: %v", err)
	}
	if total != 750_000 {
		t.Fatalf("want 750000 kept after crash window, got %d", total)
	}
	// Cap still enforced against the promoted Spent.
	if _, err := a2.Authorize(Hold{AmountMicro: 5_000_000, Domain: "other"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0}); err != ErrBudget {
		t.Fatalf("want ErrBudget after charge kept, got %v", err)
	}
}

// TestReleaseAfterMarkSignedPromotesToSpent — NEW-P3-1 / 44.repass.2:
// buggy Release after MarkSigned must keep the charge (promote to Spent).
func TestReleaseAfterMarkSignedPromotesToSpent(t *testing.T) {
	a, _ := newTestAuthority(t)
	token, err := a.Authorize(Hold{AmountMicro: 1_000_000, Domain: "example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := a.MarkSigned(token); err != nil {
		t.Fatalf("MarkSigned: %v", err)
	}
	if err := a.Release(token); err != nil {
		t.Fatalf("Release(signed): %v", err)
	}
	total, err := a.Today()
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	if total != 1_000_000 {
		t.Fatalf("want 1000000 after Release(signed) promote, got %d (must not refund)", total)
	}
	// Second Release is no-op (already Spent).
	if err := a.Release(token); err != nil {
		t.Fatalf("Release again: %v", err)
	}
	total2, _ := a.Today()
	if total2 != 1_000_000 {
		t.Fatalf("want stable 1000000, got %d", total2)
	}
}

// TestReleaseAfterMarkSignedMovesDomainTotal (51.2): promoting a signed
// reservation to Spent must move the domain ledger in the same persist.
// Today() alone hid a Release that charged the day and skipped the domain.
func TestReleaseAfterMarkSignedMovesDomainTotal(t *testing.T) {
	a, _ := newTestAuthority(t)
	const domain = "example.com"
	const amount = int64(1_000_000)
	token, err := a.Authorize(Hold{AmountMicro: amount, Domain: domain}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := a.MarkSigned(token); err != nil {
		t.Fatalf("MarkSigned: %v", err)
	}
	if err := a.Release(token); err != nil {
		t.Fatalf("Release(signed): %v", err)
	}
	today, err := a.Today()
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	dom, err := a.DomainTotal(domain)
	if err != nil {
		t.Fatalf("DomainTotal: %v", err)
	}
	if today != amount || dom != amount {
		t.Fatalf("after signed Release: Today=%d DomainTotal=%d, want both %d", today, dom, amount)
	}
	if err := a.Release(token); err != nil {
		t.Fatalf("Release again: %v", err)
	}
	today2, _ := a.Today()
	dom2, err := a.DomainTotal(domain)
	if err != nil {
		t.Fatalf("DomainTotal again: %v", err)
	}
	if today2 != amount || dom2 != amount {
		t.Fatalf("second Release doubled: Today=%d DomainTotal=%d, want both %d", today2, dom2, amount)
	}
}

// TestReleaseUnsignedStillFrees — pre-sig path must keep dropping the hold.
func TestReleaseUnsignedStillFrees(t *testing.T) {
	a, _ := newTestAuthority(t)
	token, err := a.Authorize(Hold{AmountMicro: 1_000_000, Domain: "example.com"}, Caps{DailyMicro: 5_000_000, DomainMicro: 0})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if err := a.Release(token); err != nil {
		t.Fatalf("Release(unsigned): %v", err)
	}
	total, _ := a.Today()
	if total != 0 {
		t.Fatalf("want 0 after unsigned Release, got %d", total)
	}
}
