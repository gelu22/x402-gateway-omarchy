package gateway

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func permClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// TestPermissionAllowsWithinCap (49.3): a permission pays up to its cap and no
// further — a price raise forces a fresh decision.
func TestPermissionAllowsWithinCap(t *testing.T) {
	p := NewPermissionStore(t.TempDir(), nil)
	if err := p.Add("https://seller.example/book", 10_000, false, 0); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !p.Allows("https://seller.example/book", 10_000) {
		t.Fatal("exactly the cap must be allowed")
	}
	if !p.Allows("https://seller.example/book", 5_000) {
		t.Fatal("below the cap must be allowed")
	}
	if p.Allows("https://seller.example/book", 10_001) {
		t.Fatal("one micro over the cap must ask again")
	}
	if p.Allows("https://seller.example/other", 1) {
		t.Fatal("a different URL must not be covered")
	}
}

// TestPermissionPermanentSurvivesReload (49.3): a permanent grant is on disk.
func TestPermissionPermanentSurvivesReload(t *testing.T) {
	dir := t.TempDir()
	p := NewPermissionStore(dir, nil)
	if err := p.Add("https://a.example/x", 2_000, false, 0); err != nil {
		t.Fatal(err)
	}
	again := NewPermissionStore(dir, nil)
	if !again.Allows("https://a.example/x", 2_000) {
		t.Fatal("permanent permission must survive a reload")
	}
	if len(again.List()) != 1 {
		t.Fatalf("List = %d, want 1", len(again.List()))
	}
}

// TestPermissionTemporaryExpires (49.3): "for now" does not persist and stops
// allowing once its ttl passes.
func TestPermissionTemporaryExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	p := NewPermissionStore(t.TempDir(), func() time.Time { return now })
	if err := p.Add("https://a.example/x", 2_000, true, time.Minute); err != nil {
		t.Fatal(err)
	}
	if !p.Allows("https://a.example/x", 2_000) {
		t.Fatal("temporary grant must allow before expiry")
	}
	now = now.Add(2 * time.Minute)
	if p.Allows("https://a.example/x", 2_000) {
		t.Fatal("temporary grant must stop allowing after expiry")
	}
	if len(p.List()) != 0 {
		t.Fatal("expired temporary must not be listed")
	}
}

// TestPermissionTemporaryNotOnDisk (49.3): a temporary grant never reaches disk.
func TestPermissionTemporaryNotOnDisk(t *testing.T) {
	dir := t.TempDir()
	p := NewPermissionStore(dir, nil)
	if err := p.Add("https://a.example/x", 2_000, true, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "permissions.json")); !os.IsNotExist(err) {
		t.Fatalf("temporary grant must not create permissions.json, stat err = %v", err)
	}
}

// TestPermissionRejectsBadInput (49.3): fail-closed on nonsense.
func TestPermissionRejectsBadInput(t *testing.T) {
	p := NewPermissionStore(t.TempDir(), nil)
	for _, tc := range []struct {
		name string
		url  string
		lim  int64
		temp bool
		ttl  time.Duration
	}{
		{"bad url", "ftp://a.example", 1_000, false, 0},
		{"no host", "https://", 1_000, false, 0},
		{"zero limit", "https://a.example/x", 0, false, 0},
		{"negative limit", "https://a.example/x", -5, false, 0},
		{"temp without ttl", "https://a.example/x", 1_000, true, 0},
	} {
		if err := p.Add(tc.url, tc.lim, tc.temp, tc.ttl); err == nil {
			t.Fatalf("%s: want error, got nil", tc.name)
		}
	}
	if p.Allows("https://a.example/x", 1) {
		t.Fatal("nothing was added, so nothing is allowed")
	}
}

// TestPermissionCorruptFileFailsClosed (49.3): an unreadable file grants nothing.
func TestPermissionCorruptFileFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "permissions.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := NewPermissionStore(dir, nil)
	if p.Allows("https://a.example/x", 1) {
		t.Fatal("corrupt store must grant nothing")
	}
	if len(p.List()) != 0 {
		t.Fatal("corrupt store must list nothing")
	}
}

// TestPermissionNilStoreDenies (49.3): an unwired store (minimal embeds, tests)
// denies everything instead of panicking.
func TestPermissionNilStoreDenies(t *testing.T) {
	var p *PermissionStore
	if p.Allows("https://a.example/x", 1) {
		t.Fatal("nil store must deny")
	}
	if p.List() != nil {
		t.Fatal("nil store lists nothing")
	}
}

// TestPermissionRemove (49.3): removing drops both permanent and temporary.
func TestPermissionRemove(t *testing.T) {
	p := NewPermissionStore(t.TempDir(), nil)
	_ = p.Add("https://a.example/x", 2_000, false, 0)
	_ = p.Add("https://a.example/x", 3_000, true, time.Minute)
	if !p.Remove("https://a.example/x") {
		t.Fatal("Remove must report success")
	}
	if p.Allows("https://a.example/x", 1) {
		t.Fatal("removed permission must not allow")
	}
	if p.Remove("https://a.example/x") {
		t.Fatal("second remove must report false")
	}
}

// TestPermissionHighestCapWins (49.3): when several grants match, the largest applies.
func TestPermissionHighestCapWins(t *testing.T) {
	p := NewPermissionStore(t.TempDir(), nil)
	_ = p.Add("https://a.example/x", 1_000, false, 0)
	_ = p.Add("https://a.example/x", 9_000, true, time.Minute)
	if !p.Allows("https://a.example/x", 5_000) {
		t.Fatal("the higher cap must apply")
	}
}
