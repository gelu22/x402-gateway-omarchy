//go:build linux

package install

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// RemoveOptions controls SelfRemove. KeepState/KeepConfig match
// `install.sh remove` (program only) vs full uninstall.
type RemoveOptions struct {
	Options
	KeepState  bool
	KeepConfig bool
}

// SelfRemove deletes program files this installer owns. Foreign files are kept
// with an error message (fail-closed for destructive paths).
func SelfRemove(opts RemoveOptions) error {
	if opts.PluginID == "" {
		opts.PluginID = DefaultPluginID
	}
	// A live daemon still holds the session and the signing key in memory.
	// Removing the state dir under it deletes budget.json, and Authorize on a
	// missing file starts a fresh day at Spent = 0 — a full new daily cap for a
	// process that is still signing. Refuse unless the owner forces it (46.10).
	if err := refuseLiveDaemon(opts); err != nil {
		return err
	}
	var kept []string
	binPath := filepath.Join(opts.BinDir, "gateway")
	if err := removeOurs(opts.StateDir, binPath, opts.Force); err != nil {
		if !os.IsNotExist(err) {
			kept = append(kept, binPath+": "+err.Error())
		}
	}
	for _, name := range []string{"setup-agents.sh", "remember-override.sh"} {
		p := filepath.Join(opts.ShareDir, name)
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		if err := removeOurs(opts.StateDir, p, opts.Force); err != nil {
			kept = append(kept, p+": "+err.Error())
		}
	}
	_ = os.Remove(opts.ShareDir) // only if empty

	if st, err := os.Lstat(opts.PluginDir); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			kept = append(kept, opts.PluginDir+": symlink")
		} else if id := pluginIDAt(opts.PluginDir); id == opts.PluginID || opts.Force {
			if err := removePluginDir(opts.PluginDir); err != nil {
				kept = append(kept, opts.PluginDir+": "+err.Error())
			}
		} else if id != "" {
			kept = append(kept, opts.PluginDir+": foreign plugin "+id)
		}
	}

	if !opts.KeepConfig {
		_ = os.RemoveAll(opts.ConfigDir)
	}
	if !opts.KeepState {
		_ = os.RemoveAll(opts.StateDir)
	}

	if len(kept) > 0 {
		return fmt.Errorf("install: self-remove kept foreign paths: %v", kept)
	}
	return nil
}

// daemonLive reports whether a gateway daemon still holds the singleton lock on
// <SocketPath>.lock. The lock is created if absent, so a missing lock file means
// "no daemon" rather than an error. Held open for the whole check so the answer
// cannot change under us.
func daemonLive(socketPath string) (bool, error) {
	if socketPath == "" {
		return false, nil
	}
	f, err := os.OpenFile(socketPath+".lock", os.O_RDWR|os.O_CREATE, 0o600) // #nosec G304 -- path joins user-owned stateDir (same-user trust); no remote input
	if err != nil {
		// Cannot tell. Fail closed: refusing is recoverable, deleting the
		// ledger under a live signer is not.
		return true, fmt.Errorf("install: cannot probe daemon lock %s.lock: %w", socketPath, err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true, nil // busy: a live daemon holds it
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false, nil
}

func refuseLiveDaemon(opts RemoveOptions) error {
	live, err := daemonLive(opts.SocketPath)
	if err != nil {
		return err
	}
	if !live {
		return nil
	}
	if opts.Force {
		if opts.Logger != nil {
			opts.Logger.Warn("self-remove with a live daemon: session and budget.json will be deleted from under a running process")
		}
		return nil
	}
	return fmt.Errorf("install: a gateway daemon is running (holding %s.lock) — stop it first, or pass --force to delete its session and budget anyway", opts.SocketPath)
}

func removePluginDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		if e.IsDir() {
			continue
		}
		if err := refuseSymlink(p); err != nil {
			return err
		}
		if err := os.Remove(p); err != nil {
			return err
		}
	}
	return os.Remove(dir)
}
