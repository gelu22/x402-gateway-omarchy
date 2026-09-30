//go:build linux

package install

import (
	"fmt"
	"os"
	"path/filepath"
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
