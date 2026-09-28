package spend

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixedClock(base time.Time) func() time.Time {
	return func() time.Time { return base }
}

func TestAddAndToday(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker(dir)
	if err := tr.Add(10_000); err != nil {
		t.Fatal(err)
	}
	if err := tr.Add(5_000); err != nil {
		t.Fatal(err)
	}
	if v, _ := tr.Today(); v != 15_000 {
		t.Fatalf("today = %d, want 15000", v)
	}
}

func TestDayReset(t *testing.T) {
	base := time.Date(2026, 8, 23, 23, 59, 0, 0, time.UTC)
	dir := t.TempDir()
	tr := NewTracker(dir)
	tr.now = fixedClock(base)
	_ = tr.Add(7_000)

	tr.now = fixedClock(base.Add(2 * time.Hour)) // next day
	if v, _ := tr.Today(); v != 0 {
		t.Fatalf("day rollover failed: %d", v)
	}
}

func TestLegacyBucketsMigrateToSingle(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"day":"2026-08-23","ppv_micro_usdc":7000,"ppc_micro_usdc":3000}`
	if err := os.WriteFile(filepath.Join(dir, "spend.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := NewTracker(dir)
	tr.now = fixedClock(time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC))
	if v, _ := tr.Today(); v != 10_000 {
		t.Fatalf("legacy buckets must sum into the single counter, got %d", v)
	}
}

func TestLegacyTotalMigrates(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"day":"2026-08-23","total_micro_usdc":12000}`
	if err := os.WriteFile(filepath.Join(dir, "spend.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := NewTracker(dir)
	tr.now = fixedClock(time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC))
	if v, _ := tr.Today(); v != 12_000 {
		t.Fatalf("legacy total must surface, got %d", v)
	}
}

func TestCorruptFileStartsFresh(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "spend.json"), []byte("{"), 0o600)
	tr := NewTracker(dir)
	if v, _ := tr.Today(); v != 0 {
		t.Fatalf("corrupt file should read zero, got %d", v)
	}
}

func TestTamperedNegativeClamped(t *testing.T) {
	dir := t.TempDir()
	raw := `{"day":"` + time.Now().Format("2006-01-02") + `","micro_usdc":-5000}`
	if err := os.WriteFile(filepath.Join(dir, "spend.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := NewTracker(dir)
	if v, _ := tr.Today(); v != 0 {
		t.Fatalf("negative spend must read zero, got %d", v)
	}
}

func TestFutureDayKeepsCounters(t *testing.T) {
	dir := t.TempDir()
	tomorrow := time.Now().Add(26 * time.Hour).Format("2006-01-02")
	raw := `{"day":"` + tomorrow + `","micro_usdc":42000}`
	if err := os.WriteFile(filepath.Join(dir, "spend.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := NewTracker(dir)
	if v, _ := tr.Today(); v != 42000 {
		t.Fatalf("future-dated spend must be kept, got %d", v)
	}
}

func TestSavePerms0600(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker(dir)
	if err := tr.Add(100); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "spend.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("spend.json perms %v, want 0600", perm)
	}
}

func TestMalformedDayResets(t *testing.T) {
	dir := t.TempDir()
	raw := `{"day":"zzz","micro_usdc":42000}`
	if err := os.WriteFile(filepath.Join(dir, "spend.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	tr := NewTracker(dir)
	if v, _ := tr.Today(); v != 0 {
		t.Fatalf("malformed day must reset, got %d", v)
	}
}

func TestAddRejectsNonPositive(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker(dir)
	for _, amount := range []int64{0, -100} {
		if err := tr.Add(amount); err == nil {
			t.Fatalf("Add(%d) must fail, got nil", amount)
		}
	}
}

func TestConcurrentAddNoLostUpdate(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker(dir)
	const workers = 50
	done := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() { done <- tr.Add(100) }()
	}
	for i := 0; i < workers; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if v, _ := tr.Today(); v != workers*100 {
		t.Fatalf("concurrent spend = %d, want %d", v, workers*100)
	}
}

func TestAddSaturatesAtMaxInt64(t *testing.T) {
	dir := t.TempDir()
	tr := NewTracker(dir)
	if err := tr.Add(9223372036854775800); err != nil {
		t.Fatal(err)
	}
	if err := tr.Add(100); err != nil {
		t.Fatal(err)
	}
	if v, _ := tr.Today(); v != 9223372036854775807 {
		t.Fatalf("saturate = %d, want MaxInt64", v)
	}
}
