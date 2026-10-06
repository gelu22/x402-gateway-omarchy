//go:build linux

package install

import (
	"os"
	"path/filepath"
	"testing"
)

func testOpts(t *testing.T, home, bundle, binSrc string) Options {
	t.Helper()
	return Options{
		BundleDir: bundle,
		BinarySrc: binSrc,
		BinDir:    filepath.Join(home, ".local", "bin"),
		StateDir:  filepath.Join(home, ".local", "state", "x402-gateway"),
		ShareDir:  filepath.Join(home, ".local", "share", "x402-gateway"),
		PluginDir: filepath.Join(home, ".config", "omarchy", "plugins", DefaultPluginID),
		ConfigDir: filepath.Join(home, ".config", "omarchy", "x402-gateway"),
		PluginID:  DefaultPluginID,
	}
}

func makeBundle(t *testing.T, root string) {
	t.Helper()
	dirs := []string{
		filepath.Join(root, "scripts"),
		filepath.Join(root, "plugin", "omarchy"),
		filepath.Join(root, "config"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, body string, mode os.FileMode) {
		p := filepath.Join(root, rel)
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("scripts/setup-agents.sh", "#!/bin/sh\necho stub\n", 0o755)
	write("plugin/omarchy/manifest.json", `{"id":"gelu22.gateway","version":"0.0.0"}`+"\n", 0o644)
	write("plugin/omarchy/Service.qml", "// stub\n", 0o644)
	write("plugin/omarchy/build-info.json", `{"version":"0.0.0"}`+"\n", 0o644)
	write("config/gateway-config.json", `{"paymentNetwork":"eip155:84532"}`+"\n", 0o644)
}

func makeBinary(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho gateway\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestInstallHappyPath(t *testing.T) {
	home := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle")
	bin := filepath.Join(t.TempDir(), "gateway")
	makeBundle(t, bundle)
	makeBinary(t, bin)
	_ = os.MkdirAll(filepath.Join(home, ".config", "omarchy"), 0o755)
	opts := testOpts(t, home, bundle, bin)
	if err := Install(opts); err != nil {
		t.Fatalf("Install: %v", err)
	}
	gw := filepath.Join(opts.BinDir, "gateway")
	if _, err := os.Stat(gw); err != nil {
		t.Fatal(err)
	}
	if !IsOurs(opts.StateDir, gw) {
		t.Fatal("binary not in registry")
	}
	if _, err := os.Stat(filepath.Join(opts.PluginDir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
}

func TestInstallRejectsSymlinkBinary(t *testing.T) {
	home := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle")
	bin := filepath.Join(t.TempDir(), "gateway")
	makeBundle(t, bundle)
	makeBinary(t, bin)
	opts := testOpts(t, home, bundle, bin)
	_ = os.MkdirAll(opts.BinDir, 0o755)
	if err := os.Symlink("/tmp/evil", filepath.Join(opts.BinDir, "gateway")); err != nil {
		t.Fatal(err)
	}
	if err := Install(opts); err == nil {
		t.Fatal("want symlink rejection")
	}
}

func TestInstallRejectsForeignBinary(t *testing.T) {
	home := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle")
	bin := filepath.Join(t.TempDir(), "gateway")
	makeBundle(t, bundle)
	makeBinary(t, bin)
	opts := testOpts(t, home, bundle, bin)
	_ = os.MkdirAll(opts.BinDir, 0o755)
	foreign := filepath.Join(opts.BinDir, "gateway")
	if err := os.WriteFile(foreign, []byte("foreign"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(opts); err == nil {
		t.Fatal("want foreign rejection")
	}
}

func TestInstallIdempotent(t *testing.T) {
	home := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle")
	bin := filepath.Join(t.TempDir(), "gateway")
	makeBundle(t, bundle)
	makeBinary(t, bin)
	_ = os.MkdirAll(filepath.Join(home, ".config", "omarchy"), 0o755)
	opts := testOpts(t, home, bundle, bin)
	if err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if err := Install(opts); err != nil {
		t.Fatalf("second Install: %v", err)
	}
}

func TestInstallPartialRollback(t *testing.T) {
	home := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle")
	bin := filepath.Join(t.TempDir(), "gateway")
	makeBundle(t, bundle)
	makeBinary(t, bin)
	_ = os.MkdirAll(filepath.Join(home, ".config", "omarchy"), 0o755)
	opts := testOpts(t, home, bundle, bin)
	// Make ShareDir a symlink so helper install fails after binary succeeds.
	_ = os.MkdirAll(filepath.Dir(opts.ShareDir), 0o755)
	if err := os.Symlink(t.TempDir(), opts.ShareDir); err != nil {
		t.Fatal(err)
	}
	if err := Install(opts); err == nil {
		t.Fatal("want failure on symlink share dir")
	}
	gw := filepath.Join(opts.BinDir, "gateway")
	if _, err := os.Stat(gw); !os.IsNotExist(err) {
		t.Fatalf("binary should be rolled back, exists: %v", err)
	}
}

func TestSelfRemoveAfterInstall(t *testing.T) {
	home := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle")
	bin := filepath.Join(t.TempDir(), "gateway")
	makeBundle(t, bundle)
	makeBinary(t, bin)
	_ = os.MkdirAll(filepath.Join(home, ".config", "omarchy"), 0o755)
	opts := testOpts(t, home, bundle, bin)
	if err := Install(opts); err != nil {
		t.Fatal(err)
	}
	if err := SelfRemove(RemoveOptions{Options: opts, KeepState: false, KeepConfig: false}); err != nil {
		t.Fatalf("SelfRemove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(opts.BinDir, "gateway")); !os.IsNotExist(err) {
		t.Fatal("binary leftover")
	}
	if _, err := os.Stat(opts.PluginDir); !os.IsNotExist(err) {
		t.Fatal("plugin leftover")
	}
}

func TestIsOursRegistryMatch(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(dir, "setup-agents.sh")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho ours\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if IsOurs(state, helper) {
		t.Fatal("unregistered helper must not be ours")
	}
	if err := registrySet(state, helper); err != nil {
		t.Fatal(err)
	}
	if !IsOurs(state, helper) {
		t.Fatal("registered helper must be ours")
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho foreign\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if IsOurs(state, helper) {
		t.Fatal("tampered helper must not be ours")
	}
}

func TestRegistryTraversalBlocked(t *testing.T) {
	// Symlink at StateDir itself → openRoot refuses (refuseSymlink).
	base := t.TempDir()
	realState := filepath.Join(base, "real-state")
	if err := os.MkdirAll(realState, 0o700); err != nil {
		t.Fatal(err)
	}
	linkState := filepath.Join(base, "link-state")
	if err := os.Symlink(realState, linkState); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(base, "bin.sh")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := registrySet(linkState, helper); err == nil {
		t.Fatal("registrySet through symlink StateDir must fail")
	}

	// os.Root rejects relative escape from StateDir.
	state := filepath.Join(base, "state")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "secret.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := openRoot(state)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := root.Open("../secret.txt"); err == nil {
		t.Fatal("root.Open(../secret.txt) must fail")
	}
	if _, err := root.ReadFile("../secret.txt"); err == nil {
		t.Fatal("root.ReadFile(../secret.txt) must fail")
	}
}
