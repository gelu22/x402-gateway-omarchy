package gateway

import (
	"os"
	"testing"
)

// TestAddPersistFailKeepsToday (NEW-P1-1 / 44.7.1): a write error after bump
// must not loosen domain spend — Today still sees the charge (dirty memory).
func TestAddPersistFailKeepsToday(t *testing.T) {
	dir := t.TempDir()
	r := NewSellerRegistry(dir)
	const domain = "seller.example.com"
	if err := r.Add(domain, 10_000); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	got, err := r.Today(domain)
	if err != nil || got != 10_000 {
		t.Fatalf("Today after first Add = %d, want 10000 (err %v)", got, err)
	}

	// Block tmp+rename into the state dir (same pattern as budget fail-closed).
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := r.Add(domain, 5_000); err == nil {
		t.Fatal("second Add on read-only dir: want persist error, got nil")
	}
	got, err = r.Today(domain)
	if err != nil {
		t.Fatalf("Today after failed Add: %v", err)
	}
	if got != 15_000 {
		t.Fatalf("Today after failed persist = %d, want 15000 (dirty must keep both charges)", got)
	}
}

// TestAddPersistFailThenRecoverNoDoubleCount: when the dir becomes writable
// again, a successful Add must not double-apply the dirty bump.
func TestAddPersistFailThenRecoverNoDoubleCount(t *testing.T) {
	dir := t.TempDir()
	r := NewSellerRegistry(dir)
	const domain = "seller.example.com"
	if err := r.Add(domain, 10_000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(domain, 5_000); err == nil {
		t.Fatal("want persist error")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Dirty still 15k; one more 1k → 16k on disk and in Today (not 15k+15k+1k).
	if err := r.Add(domain, 1_000); err != nil {
		t.Fatalf("recover Add: %v", err)
	}
	got, err := r.Today(domain)
	if err != nil || got != 16_000 {
		t.Fatalf("Today after recover = %d, want 16000 (err %v)", got, err)
	}
	// Fresh registry from disk must match (persist caught up).
	r2 := NewSellerRegistry(dir)
	got2, err := r2.Today(domain)
	if err != nil || got2 != 16_000 {
		t.Fatalf("disk Today = %d, want 16000 (err %v)", got2, err)
	}
}
