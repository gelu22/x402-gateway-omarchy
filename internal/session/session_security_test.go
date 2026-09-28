package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T3: a corrupt session file must yield LoggedOut (not crash) and the store
// must never loosen its permissions.
func TestSecurityCorruptSessionLoggedOut(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")
	if err := os.WriteFile(path, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewStore(dir).Load()
	if err == nil {
		t.Fatal("want error on corrupt session")
	}
	m, err := New(nil, NewStore(dir), nil)
	if err != nil {
		t.Fatalf("New must not fail on corrupt session: %v", err)
	}
	state, _, _ := m.Status()
	if state != StateLoggedOut {
		t.Fatalf("state = %s, want logged_out", state)
	}
}

// T3: session.json must be 0600 after every save.
func TestSecuritySessionFilePerms(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Save(&Data{UserID: "u", RefreshToken: "r"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "session.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("session.json perms %v, want 0600", perm)
	}
}

// T3: tokens must never appear in persisted data other than the refresh token.
func TestSecurityNoAccessTokenOnDisk(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	if err := st.Save(&Data{UserID: "u", RefreshToken: "refresh-secret"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "session.json"))
	for _, forbidden := range []string{"access_token", "wallet_secret", "private_key"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("session.json contains forbidden key %q", forbidden)
		}
	}
}

// 23.5: logout must wipe the key buffer (bytes, not semantics) and drop the
// reference. The byte-level proof lives in internal/cdp (same-package access to
// the scalar buffer); here we assert the session-level guarantees.
func TestSecurityLogoutWipesTWSKey(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, err := New(f.client(), NewStore(dir), slogNop())
	if err != nil {
		t.Fatal(err)
	}
	ws, err := m.WalletSecret()
	if err != nil {
		t.Fatalf("WalletSecret: %v", err)
	}
	if !ws.HasKey() {
		t.Fatal("test needs a live key")
	}

	m.logoutLocked()

	if m.tws != nil {
		t.Fatal("logout must drop the TWS reference")
	}
	if ws.HasKey() {
		t.Error("logout must wipe the key buffer")
	}
	if ws.PublicSPKI != nil {
		t.Error("logout must clear PublicSPKI")
	}
}

// 23.5: the shutdown path calls DropTWS() explicitly (os.Exit skips defers).
func TestSecurityDropTWSOnShutdownPath(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, err := New(f.client(), NewStore(dir), slogNop())
	if err != nil {
		t.Fatal(err)
	}
	ws, err := m.WalletSecret()
	if err != nil {
		t.Fatalf("WalletSecret: %v", err)
	}

	m.DropTWS()

	if m.tws != nil {
		t.Fatal("DropTWS must drop the TWS reference")
	}
	if ws.HasKey() {
		t.Error("DropTWS must wipe the key buffer")
	}
}
