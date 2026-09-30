//go:build linux

package install

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

const DefaultPluginID = "gelu22.gateway"

// Options names every destination the installer may touch. Home is expanded
// by the CLI; Absolute paths only.
type Options struct {
	BundleDir string // extracted plugin-bundle root
	BinarySrc string // verified downloaded gateway binary
	BinDir    string
	StateDir  string
	ShareDir  string
	PluginDir string
	ConfigDir string
	PluginID  string
	Force     bool
	Logger    *slog.Logger
}

// Install places the binary, helpers, plugin and config seed. On mid-flight
// failure it best-effort rolls back files it already wrote.
func Install(opts Options) error {
	if opts.PluginID == "" {
		opts.PluginID = DefaultPluginID
	}
	if opts.BinarySrc == "" || opts.BundleDir == "" {
		return fmt.Errorf("install: BinarySrc and BundleDir required")
	}
	var done []string
	rollback := func(err error) error {
		for i := len(done) - 1; i >= 0; i-- {
			_ = removeOurs(opts.StateDir, done[i], true)
		}
		if opts.Logger != nil {
			opts.Logger.Error("install rollback", "err", err)
		}
		return err
	}

	if err := os.MkdirAll(opts.StateDir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(opts.StateDir, 0o700)
	if err := refuseSymlink(opts.StateDir); err != nil {
		return err
	}

	binPath := filepath.Join(opts.BinDir, "gateway")
	if err := refuseSymlink(binPath); err != nil {
		return err
	}
	if err := ownedByUs(binPath); err != nil {
		return err
	}
	if _, err := os.Lstat(binPath); err == nil && !opts.Force && !IsOurs(opts.StateDir, binPath) {
		return fmt.Errorf("%s already exists and was not installed by this installer", binPath)
	}
	if err := installFileAtomically(opts.BinarySrc, opts.BinDir, "gateway", 0o755); err != nil {
		return rollback(err)
	}
	if err := registrySet(opts.StateDir, binPath); err != nil {
		return rollback(err)
	}
	done = append(done, binPath)

	helperSrc := filepath.Join(opts.BundleDir, "scripts", "setup-agents.sh")
	helperDst := filepath.Join(opts.ShareDir, "setup-agents.sh")
	if _, err := os.Stat(helperSrc); err == nil {
		if err := refuseSymlink(helperDst); err != nil {
			return rollback(err)
		}
		if _, err := os.Lstat(helperDst); err == nil && !opts.Force && !IsOurs(opts.StateDir, helperDst) {
			return rollback(fmt.Errorf("%s already exists and was not installed by this installer", helperDst))
		}
		if err := installFileAtomically(helperSrc, opts.ShareDir, "setup-agents.sh", 0o755); err != nil {
			return rollback(err)
		}
		if err := registrySet(opts.StateDir, helperDst); err != nil {
			return rollback(err)
		}
		done = append(done, helperDst)
	}
	// Retired helper: drop only when ours.
	retired := filepath.Join(opts.ShareDir, "remember-override.sh")
	if _, err := os.Lstat(retired); err == nil {
		if IsOurs(opts.StateDir, retired) || opts.Force {
			_ = removeOurs(opts.StateDir, retired, true)
		}
	}

	omarchyRoot := filepath.Dir(filepath.Dir(opts.PluginDir)) // .../omarchy
	if st, err := os.Stat(omarchyRoot); err == nil && st.IsDir() {
		if err := installPlugin(opts); err != nil {
			return rollback(err)
		}
		if err := seedConfig(opts); err != nil {
			return rollback(err)
		}
	} else {
		fmt.Printf("  ⚠ no ~/.config/omarchy — skipping QML plugin (manual: copy plugin/omarchy/ to ~/.config/omarchy/plugins/%s)\n", opts.PluginID)
		fmt.Println("  ⚠ no ~/.config/omarchy — skipping plugin config seed")
	}
	return nil
}

func installPlugin(opts Options) error {
	if err := refuseSymlink(opts.PluginDir); err != nil {
		return err
	}
	if st, err := os.Lstat(opts.PluginDir); err == nil {
		if !st.IsDir() {
			return fmt.Errorf("%s exists and is not a directory — refusing", opts.PluginDir)
		}
		if id := pluginIDAt(opts.PluginDir); id != "" && id != opts.PluginID {
			return fmt.Errorf("%s holds plugin '%s' — refusing to overwrite", opts.PluginDir, id)
		}
	}
	srcDir := filepath.Join(opts.BundleDir, "plugin", "omarchy")
	if err := os.MkdirAll(opts.PluginDir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(name, ".qml") && !strings.HasSuffix(name, ".js") &&
			name != "manifest.json" && name != "build-info.json" {
			continue
		}
		src := filepath.Join(srcDir, name)
		mode := uint32(0o644)
		if err := installFileAtomically(src, opts.PluginDir, name, mode); err != nil {
			return err
		}
	}
	return nil
}

func seedConfig(opts Options) error {
	cfgFile := filepath.Join(opts.ConfigDir, "config.json")
	if _, err := os.Stat(cfgFile); err == nil {
		return nil // keep existing
	}
	src := filepath.Join(opts.BundleDir, "config", "gateway-config.json")
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	if err := os.MkdirAll(opts.ConfigDir, 0o700); err != nil {
		return err
	}
	_ = os.Chmod(opts.ConfigDir, 0o700)
	return installFileAtomically(src, opts.ConfigDir, "config.json", 0o600)
}

func pluginIDAt(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return ""
	}
	var m struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return m.ID
}
