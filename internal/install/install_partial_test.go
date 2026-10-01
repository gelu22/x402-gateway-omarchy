package install

import (
	"os"
	"path/filepath"
	"testing"
)

// pluginFiles lists what a successful installPlugin leaves in PluginDir.
func pluginFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestInstallPluginRollsBackWrittenFiles (46.9, F4): a failure after some plugin
// files are already written must not leave a half-copied plugin. A manifest
// without its QML files is a panel that never opens.
func TestInstallPluginRollsBackWrittenFiles(t *testing.T) {
	home := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle")
	makeBundle(t, bundle)
	pluginSrc := filepath.Join(bundle, "plugin", "omarchy")
	// Sorted order puts Aaa.qml first (written), Zzz.qml last (fails).
	if err := os.WriteFile(filepath.Join(pluginSrc, "Aaa.qml"), []byte("import QtQuick\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(pluginSrc, "Zzz.qml")
	if err := os.WriteFile(blocked, []byte("import QtQuick\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unreadable source: os.Open fails for this uid, so the loop dies at Zzz.
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o644) })

	opts := testOpts(t, home, bundle, "")
	opts.PluginDir = filepath.Join(t.TempDir(), "plugin")
	if err := os.MkdirAll(opts.PluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	err := installPlugin(opts)
	if err == nil {
		t.Fatal("want failure on unreadable bundle member")
	}
	if left := pluginFiles(t, opts.PluginDir); len(left) != 0 {
		t.Fatalf("half-installed plugin left behind: %v", left)
	}
}

// TestInstallPluginReadOnlyDirLeavesForeignContent (46.9, rule 7a): a
// read-only destination must fail loudly and leave pre-existing files alone.
func TestInstallPluginReadOnlyDirLeavesForeignContent(t *testing.T) {
	home := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle")
	makeBundle(t, bundle)

	opts := testOpts(t, home, bundle, "")
	opts.PluginDir = filepath.Join(t.TempDir(), "plugin")
	if err := os.MkdirAll(opts.PluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(opts.PluginDir, "SomeoneElse.qml")
	if err := os.WriteFile(foreign, []byte("// not ours\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(opts.PluginDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(opts.PluginDir, 0o755) })

	if err := installPlugin(opts); err == nil {
		t.Fatal("want failure on a read-only plugin dir")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("foreign file must survive a failed install: %v", err)
	}
}

// TestInstallPluginFailureKeepsBinaryInstallIntact (46.9): cleanup is scoped to
// the plugin directory, so a plugin failure must not undo the already-installed
// binary bookkeeping in a way that hides the real error.
func TestInstallPluginForeignManifestStillRefused(t *testing.T) {
	home := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "bundle")
	makeBundle(t, bundle)

	opts := testOpts(t, home, bundle, "")
	opts.PluginDir = filepath.Join(t.TempDir(), "plugin")
	if err := os.MkdirAll(opts.PluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opts.PluginDir, "manifest.json"),
		[]byte(`{"id":"someone.else"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installPlugin(opts); err == nil {
		t.Fatal("want refusal on a foreign plugin dir")
	}
	if left := pluginFiles(t, opts.PluginDir); len(left) != 1 || left[0] != "manifest.json" {
		t.Fatalf("foreign dir must be untouched, got %v", left)
	}
}
