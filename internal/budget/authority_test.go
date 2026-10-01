package budget

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestAuthority(t *testing.T) (*Authority, string) {
	t.Helper()
	dir := t.TempDir()
	return NewAuthority(dir, time.Now), dir
}

// TestAuthorizeRejectsOverCap proves a single authorization above the cap fails.
func TestAuthorizeRejectsOverCap(t *testing.T) {
	a, _ := newTestAuthority(t)
	if _, err := a.Authorize(6_000_000, 5_000_000, 0, "example.com"); err != ErrBudget {
		t.Fatalf("want ErrBudget, got %v", err)
	}
}

// TestAuthorizeAcceptsWithinCap proves a normal authorization succeeds and persists.
func TestAuthorizeAcceptsWithinCap(t *testing.T) {
	a, _ := newTestAuthority(t)
	token, err := a.Authorize(1_000_000, 5_000_000, 0, "example.com")
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	total, err := a.Today()
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	if total != 1_000_000 {
		t.Fatalf("want 1000000, got %d", total)
	}
}

// TestConcurrentAuthorizeNeverExceedsCap is the core race invariant: N
// goroutines authorizing against the same cap — the sum of all accepted
// authorizations must never exceed the cap.
func TestConcurrentAuthorizeNeverExceedsCap(t *testing.T) {
	a, _ := newTestAuthority(t)
	capMicro := int64(5_000_000)
	amount := int64(100_000) // 50 × 100k = 5M = exactly the cap
	var mu sync.Mutex
	accepted := int64(0)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.Authorize(amount, capMicro, 0, "example.com")
			if err == nil {
				mu.Lock()
				accepted += amount
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if accepted > capMicro {
		t.Fatalf("concurrent authorizations exceeded cap: %d > %d", accepted, capMicro)
	}
	total, _ := a.Today()
	if total > capMicro {
		t.Fatalf("Today() exceeded cap: %d > %d", total, capMicro)
	}
}

// TestConcurrentAuthorizeSubCapNeverExceeds proves the per-domain sub-cap
// holds under concurrency on the same domain.
func TestConcurrentAuthorizeSubCapNeverExceeds(t *testing.T) {
	a, _ := newTestAuthority(t)
	capMicro := int64(10_000_000)
	subcap := int64(1_000_000)
	amount := int64(100_000) // 10 × 100k = 1M = exactly the sub-cap
	var mu sync.Mutex
	accepted := int64(0)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.Authorize(amount, capMicro, subcap, "same.example.com")
			if err == nil {
				mu.Lock()
				accepted += amount
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if accepted > subcap {
		t.Fatalf("sub-cap exceeded: %d > %d", accepted, subcap)
	}
}

// TestAuthorizeFailClosedOnWriteError proves a write failure means no
// authorization (fail-closed): the caller must not sign.
func TestAuthorizeFailClosedOnWriteError(t *testing.T) {
	dir := t.TempDir()
	// Make the state dir read-only after creating it.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	defer os.Chmod(dir, 0o700) //nolint:errcheck
	a := NewAuthority(dir, time.Now)
	_, err := a.Authorize(1_000_000, 5_000_000, 0, "example.com")
	if err == nil {
		t.Fatal("want error on read-only dir, got nil")
	}
	if err == ErrBudget {
		t.Fatal("got ErrBudget, want a write error (fail-closed, not budget)")
	}
}

// TestCommitMovesReservedToSpent proves Commit charges the amount.
func TestCommitMovesReservedToSpent(t *testing.T) {
	a, _ := newTestAuthority(t)
	token, _ := a.Authorize(1_000_000, 5_000_000, 0, "example.com")
	if err := a.Commit(token); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	total, _ := a.Today()
	if total != 1_000_000 {
		t.Fatalf("want 1000000 after commit, got %d", total)
	}
}

// TestReleaseFreesBudget proves Release returns the amount to the pool.
func TestReleaseFreesBudget(t *testing.T) {
	a, _ := newTestAuthority(t)
	token, _ := a.Authorize(1_000_000, 5_000_000, 0, "example.com")
	if err := a.Release(token); err != nil {
		t.Fatalf("Release: %v", err)
	}
	total, _ := a.Today()
	if total != 0 {
		t.Fatalf("want 0 after release, got %d", total)
	}
}

// TestCommitReleaseIdempotent proves unknown tokens are no-ops.
func TestCommitReleaseIdempotent(t *testing.T) {
	a, _ := newTestAuthority(t)
	if err := a.Commit("nonexistent"); err != nil {
		t.Fatalf("Commit(nonexistent): %v", err)
	}
	if err := a.Release("nonexistent"); err != nil {
		t.Fatalf("Release(nonexistent): %v", err)
	}
}

// TestTTLUnsignedDrops proves an unsigned reservation older than the TTL
// is freed on the next load (MFA abandon / crash-before-sig).
func TestTTLUnsignedDrops(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := NewAuthority(dir, func() time.Time { return now })
	if _, err := a.Authorize(1_000_000, 5_000_000, 0, "example.com"); err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	total, _ := a.Today()
	if total != 1_000_000 {
		t.Fatalf("want 1000000, got %d", total)
	}
	a2 := NewAuthority(dir, func() time.Time { return now.Add(ReservationTTL + time.Minute) })
	total2, _ := a2.Today()
	if total2 != 0 {
		t.Fatalf("want 0 after unsigned TTL sweep, got %d", total2)
	}
}

// TestDayRolloverResetsBudget proves a new day starts fresh.
func TestDayRolloverResetsBudget(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	a := NewAuthority(dir, func() time.Time { return now })
	token, _ := a.Authorize(1_000_000, 5_000_000, 0, "example.com")
	_ = a.Commit(token)
	// Next day: budget resets.
	a2 := NewAuthority(dir, func() time.Time { return now.Add(24 * time.Hour) })
	total, _ := a2.Today()
	if total != 0 {
		t.Fatalf("want 0 after day rollover, got %d", total)
	}
}

// TestCorruptFileFailsClosed proves a corrupt budget.json is treated as a
// fresh day (caps still enforced, not zeroed to unlimited).
//
// Precise scope, because the name overstates it (46.12): what is proven is
// that Authorize still enforces the cap after a corrupt file, i.e. the cap is
// not disabled. What is NOT proven, and is a known open residual, is that the
// previous Spent survives — it does not: load() returns a fresh day and the
// earlier total is lost silently. The test name predates that distinction; see
// THREAT-MODEL T3 (residual row) and knowledge/sessions/44.2-findings-checkpoint.md.
func TestCorruptFileFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "budget.json"), []byte("garbage"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	a := NewAuthority(dir, time.Now)
	// Corrupt file → fresh day → Authorize works within cap.
	if _, err := a.Authorize(1_000_000, 5_000_000, 0, "example.com"); err != nil {
		t.Fatalf("Authorize after corrupt: %v", err)
	}
	// But over cap still fails.
	if _, err := a.Authorize(5_000_000, 5_000_000, 0, "example.com"); err != ErrBudget {
		t.Fatalf("want ErrBudget after corrupt, got %v", err)
	}
}

// TestSubCapDisabledWhenZero proves subcapMicro=0 skips the domain check.
func TestSubCapDisabledWhenZero(t *testing.T) {
	a, _ := newTestAuthority(t)
	// 3 × 2M = 6M > 5M cap, but sub-cap is 0 (disabled) — only the cap matters.
	_, err := a.Authorize(2_000_000, 5_000_000, 0, "example.com")
	if err != nil {
		t.Fatalf("Authorize with subcap=0: %v", err)
	}
	_, err = a.Authorize(2_000_000, 5_000_000, 0, "example.com")
	if err != nil {
		t.Fatalf("second Authorize: %v", err)
	}
	_, err = a.Authorize(2_000_000, 5_000_000, 0, "example.com")
	if err != ErrBudget {
		t.Fatalf("third Authorize: want ErrBudget (6M > 5M), got %v", err)
	}
}
