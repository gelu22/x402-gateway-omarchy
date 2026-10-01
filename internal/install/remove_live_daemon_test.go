//go:build linux

package install

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// holdDaemonLock takes the same exclusive flock the running daemon holds, and
// returns a release func. Without a real lock the test would not exercise the
// busy path at all.
func holdDaemonLock(t *testing.T, socketPath string) func() {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(socketPath+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("could not take the daemon lock: %v", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}

func removeOpts(t *testing.T) RemoveOptions {
	t.Helper()
	home := t.TempDir()
	return RemoveOptions{
		Options: Options{
			BinDir:     filepath.Join(home, ".local", "bin"),
			StateDir:   filepath.Join(home, ".local", "state", "x402-gateway"),
			ShareDir:   filepath.Join(home, ".local", "share", "x402-gateway"),
			PluginDir:  filepath.Join(home, ".config", "omarchy", "plugins", DefaultPluginID),
			ConfigDir:  filepath.Join(home, ".config", "omarchy", "x402-gateway"),
			PluginID:   DefaultPluginID,
			SocketPath: filepath.Join(home, ".local", "state", "x402-gateway", "gw.sock"),
		},
	}
}

// TestSelfRemoveRefusesLiveDaemon (46.10): deleting the state dir under a live
// daemon removes budget.json, and Authorize on a missing file starts a fresh day
// at Spent = 0 — a full new daily cap for a process that still holds the signing
// key.
func TestSelfRemoveRefusesLiveDaemon(t *testing.T) {
	opts := removeOpts(t)
	for _, d := range []string{opts.StateDir, opts.ShareDir, opts.BinDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	budget := filepath.Join(opts.StateDir, "budget.json")
	if err := os.WriteFile(budget, []byte(`{"day":"2026-10-01","spent_micro":1234}`), 0o600); err != nil {
		t.Fatal(err)
	}
	release := holdDaemonLock(t, opts.SocketPath)
	defer release()

	if err := SelfRemove(opts); err == nil {
		t.Fatal("want refusal while a daemon holds the lock")
	}
	if _, err := os.Stat(budget); err != nil {
		t.Fatalf("budget.json must survive a refused self-remove: %v", err)
	}
	if _, err := os.Stat(opts.StateDir); err != nil {
		t.Fatalf("state dir must survive a refused self-remove: %v", err)
	}
}

// TestSelfRemoveForceProceedsWithLiveDaemon: --force is the owner saying yes.
func TestSelfRemoveForceProceedsWithLiveDaemon(t *testing.T) {
	opts := removeOpts(t)
	opts.Force = true
	for _, d := range []string{opts.StateDir, opts.ShareDir, opts.BinDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	budget := filepath.Join(opts.StateDir, "budget.json")
	if err := os.WriteFile(budget, []byte(`{"day":"2026-10-01","spent_micro":1234}`), 0o600); err != nil {
		t.Fatal(err)
	}
	release := holdDaemonLock(t, opts.SocketPath)
	defer release()

	if err := SelfRemove(opts); err != nil {
		t.Fatalf("--force must proceed: %v", err)
	}
	if _, err := os.Stat(budget); !os.IsNotExist(err) {
		t.Fatalf("--force should have removed the ledger, stat err = %v", err)
	}
}

// TestSelfRemoveWithoutLiveDaemonProceeds: the guard must not block the normal
// case, including when no lock file exists yet.
func TestSelfRemoveWithoutLiveDaemonProceeds(t *testing.T) {
	opts := removeOpts(t)
	for _, d := range []string{opts.StateDir, opts.ShareDir, opts.BinDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(opts.SocketPath + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("precondition: no lock file, stat err = %v", err)
	}
	if err := SelfRemove(opts); err != nil {
		t.Fatalf("self-remove with no daemon must proceed: %v", err)
	}
}

// TestSelfRemoveEmptySocketPathSkipsProbe: a caller that does not wire
// SocketPath (older embedders) must not be blocked by a probe it cannot make.
func TestSelfRemoveEmptySocketPathSkipsProbe(t *testing.T) {
	opts := removeOpts(t)
	opts.SocketPath = ""
	for _, d := range []string{opts.StateDir, opts.ShareDir, opts.BinDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := SelfRemove(opts); err != nil {
		t.Fatalf("empty SocketPath must skip the probe: %v", err)
	}
}
