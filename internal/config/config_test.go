package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load("v0.1.0", "", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Version != "v0.1.0" {
		t.Errorf("Version = %q, want %q", cfg.Version, "v0.1.0")
	}
	if cfg.Network != defaultNetwork {
		t.Errorf("Network = %q, want %q", cfg.Network, defaultNetwork)
	}
	if cfg.StateDir != dir {
		t.Errorf("StateDir = %q, want %q", cfg.StateDir, dir)
	}
	if cfg.ProjectID != defaultProjectID {
		t.Errorf("ProjectID = %q, want %q", cfg.ProjectID, defaultProjectID)
	}
	if cfg.CDPBaseURL != "" {
		t.Errorf("CDPBaseURL = %q, want empty", cfg.CDPBaseURL)
	}
	if cfg.Address != "" {
		t.Errorf("Address = %q, want empty", cfg.Address)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat state dir: %v", err)
	}
	if info.Mode().Perm() != stateDirPerms {
		t.Errorf("State dir perms = %o, want %o", info.Mode().Perm(), stateDirPerms)
	}
}

func TestLoadFlagOverridesEnv(t *testing.T) {
	dir := t.TempDir()
	otherDir := t.TempDir()
	t.Setenv("GATEWAY_SOCKET_PATH", filepath.Join(dir, "gw.sock"))
	t.Setenv("GATEWAY_STATE_DIR", dir)
	t.Setenv("GATEWAY_NETWORK", "eip155:8453")
	t.Setenv("GATEWAY_EVM_ADDRESS", "0x1234567890123456789012345678901234567890")
	t.Setenv("CDP_PROJECT_ID", "custom-project-id")
	t.Setenv("CDP_BASE_URL", "https://custom.cdp.example.com")

	cfg, err := Load("v1.0.0", filepath.Join(otherDir, "flag.sock"), otherDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SocketPath != filepath.Join(otherDir, "flag.sock") {
		t.Errorf("SocketPath = %q, want %q (flag > env)", cfg.SocketPath, filepath.Join(otherDir, "flag.sock"))
	}
	if cfg.StateDir != otherDir {
		t.Errorf("StateDir = %q, want %q (flag > env)", cfg.StateDir, otherDir)
	}
	if cfg.Network != "eip155:8453" {
		t.Errorf("Network = %q, want %q", cfg.Network, "eip155:8453")
	}
	if cfg.Address != "0x1234567890123456789012345678901234567890" {
		t.Errorf("Address = %q, want %q", cfg.Address, "0x1234567890123456789012345678901234567890")
	}
	if cfg.ProjectID != "custom-project-id" {
		t.Errorf("ProjectID = %q, want %q", cfg.ProjectID, "custom-project-id")
	}
	if cfg.CDPBaseURL != "https://custom.cdp.example.com" {
		t.Errorf("CDPBaseURL = %q, want %q", cfg.CDPBaseURL, "https://custom.cdp.example.com")
	}
}

func TestLoadInvalidNetwork(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEWAY_NETWORK", "invalid-network")
	_, err := Load("v0.1.0", "", dir)
	if err == nil {
		t.Fatal("Load with invalid network: want error, got nil")
	}
	if got := err.Error(); got != `config: unsupported network "invalid-network" (want CAIP-2 eip155:<chain>)` {
		t.Errorf("Error = %q, want %q", got, `config: unsupported network "invalid-network" (want CAIP-2 eip155:<chain>)`)
	}
}

func TestLoadEnvNetwork(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEWAY_NETWORK", "eip155:8453")
	cfg, err := Load("v0.1.0", "", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Network != "eip155:8453" {
		t.Errorf("Network = %q, want %q", cfg.Network, "eip155:8453")
	}
}

func TestLoadEnvSocketPath(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "env.sock")
	t.Setenv("GATEWAY_SOCKET_PATH", sockPath)
	cfg, err := Load("v0.1.0", "", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SocketPath != sockPath {
		t.Errorf("SocketPath = %q, want %q", cfg.SocketPath, sockPath)
	}
}

func TestLoadEnvStateDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEWAY_STATE_DIR", dir)
	cfg, err := Load("v0.1.0", "", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.StateDir != dir {
		t.Errorf("StateDir = %q, want %q", cfg.StateDir, dir)
	}
}

func TestLoadEnvEvmAddress(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEWAY_EVM_ADDRESS", "  0xAbC1234567890AbC1234567890AbC1234567890  ")
	cfg, err := Load("v0.1.0", "", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Address != "0xAbC1234567890AbC1234567890AbC1234567890" {
		t.Errorf("Address = %q, want trimmed address", cfg.Address)
	}
}

func TestLoadEnvCdpProjectId(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CDP_PROJECT_ID", "env-project-id")
	cfg, err := Load("v0.1.0", "", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ProjectID != "env-project-id" {
		t.Errorf("ProjectID = %q, want %q", cfg.ProjectID, "env-project-id")
	}
}

func TestLoadEnvCdpBaseUrl(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CDP_BASE_URL", "https://test.cdp.example.com")
	cfg, err := Load("v0.1.0", "", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CDPBaseURL != "https://test.cdp.example.com" {
		t.Errorf("CDPBaseURL = %q, want %q", cfg.CDPBaseURL, "https://test.cdp.example.com")
	}
}

func TestLoadXdgStateDir(t *testing.T) {
	xdgDir := t.TempDir()
	t.Setenv("XDG_STATE_DIR", xdgDir)
	// stateRoot is tested separately, but Load uses it indirectly
	// We verify that Load doesn't crash when XDG_STATE_DIR is set
	cfg, err := Load("v0.1.0", "", "")
	if err != nil {
		t.Fatalf("Load with XDG_STATE_DIR: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil config")
	}
}

func TestStateRootDefault(t *testing.T) {
	// Ensure XDG_STATE_DIR is unset for this test
	t.Setenv("XDG_STATE_DIR", "")
	root := stateRoot()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("UserHomeDir failed: %v", err)
	}
	want := filepath.Join(home, ".local", "state")
	if root != want {
		t.Errorf("stateRoot() = %q, want %q", root, want)
	}
}

func TestStateRootXdgOverride(t *testing.T) {
	xdgDir := "/custom/xdg/state"
	t.Setenv("XDG_STATE_DIR", xdgDir)
	root := stateRoot()
	if root != xdgDir {
		t.Errorf("stateRoot() = %q, want %q", root, xdgDir)
	}
}

func TestEnsureStateDirCreate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "newdir")
	err := ensureStateDir(dir)
	if err != nil {
		t.Fatalf("ensureStateDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("state dir is not a directory")
	}
	if info.Mode().Perm() != stateDirPerms {
		t.Errorf("perms = %o, want %o", info.Mode().Perm(), stateDirPerms)
	}
}

func TestEnsureStateDirChmod(t *testing.T) {
	dir := t.TempDir()
	// Set wrong permissions
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	err := ensureStateDir(dir)
	if err != nil {
		t.Fatalf("ensureStateDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != stateDirPerms {
		t.Errorf("perms = %o, want %o (should be corrected)", info.Mode().Perm(), stateDirPerms)
	}
}

func TestEnsureStateDirAlreadyCorrect(t *testing.T) {
	dir := t.TempDir()
	// Already has correct permissions
	if err := os.Chmod(dir, stateDirPerms); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	err := ensureStateDir(dir)
	if err != nil {
		t.Fatalf("ensureStateDir: %v", err)
	}
}

func TestEnvOrNonEmptyEnv(t *testing.T) {
	t.Setenv("TEST_ENV_OR_KEY", "env-value")
	got := envOr("TEST_ENV_OR_KEY", "fallback")
	if got != "env-value" {
		t.Errorf("envOr() = %q, want %q", got, "env-value")
	}
}

func TestEnvOrEmptyEnv(t *testing.T) {
	t.Setenv("TEST_ENV_OR_KEY", "  ")
	got := envOr("TEST_ENV_OR_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("envOr() = %q, want %q (whitespace-only treated as empty)", got, "fallback")
	}
}

func TestEnvOrUnsetEnv(t *testing.T) {
	t.Setenv("TEST_ENV_OR_KEY", "")
	got := envOr("TEST_ENV_OR_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("envOr() = %q, want %q (unset env)", got, "fallback")
	}
}

func TestEnvOrNonEmptyValue(t *testing.T) {
	// Env not set, should return fallback
	got := envOr("NONEXISTENT_ENV_VAR_12345", "fallback-value")
	if got != "fallback-value" {
		t.Errorf("envOr() = %q, want %q", got, "fallback-value")
	}
}

func TestFirstNonEmptyFirst(t *testing.T) {
	got := firstNonEmpty("first", "second", "third")
	if got != "first" {
		t.Errorf("firstNonEmpty() = %q, want %q", got, "first")
	}
}

func TestFirstNonEmptySkipEmpty(t *testing.T) {
	got := firstNonEmpty("", "second", "third")
	if got != "second" {
		t.Errorf("firstNonEmpty() = %q, want %q", got, "second")
	}
}

func TestFirstNonEmptySkipWhitespace(t *testing.T) {
	got := firstNonEmpty("  ", "\t", "third")
	if got != "third" {
		t.Errorf("firstNonEmpty() = %q, want %q (whitespace skipped)", got, "third")
	}
}

func TestFirstNonEmptyAllEmpty(t *testing.T) {
	got := firstNonEmpty("", "", "")
	if got != "" {
		t.Errorf("firstNonEmpty() = %q, want empty", got)
	}
}

func TestFirstNonEmptySingle(t *testing.T) {
	got := firstNonEmpty("only")
	if got != "only" {
		t.Errorf("firstNonEmpty() = %q, want %q", got, "only")
	}
}

func TestFirstNonEmptyNilSlice(t *testing.T) {
	got := firstNonEmpty()
	if got != "" {
		t.Errorf("firstNonEmpty() = %q, want empty", got)
	}
}

func TestFirstNonEmptyMixed(t *testing.T) {
	got := firstNonEmpty("", "  ", "real", "", "other")
	if got != "real" {
		t.Errorf("firstNonEmpty() = %q, want %q", got, "real")
	}
}

func TestLoadInvalidNetworkFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GATEWAY_NETWORK", "bad-network")
	_, err := Load("v0.1.0", "", dir)
	if err == nil {
		t.Fatal("Load with invalid network from env: want error, got nil")
	}
	want := `config: unsupported network "bad-network" (want CAIP-2 eip155:<chain>)`
	if got := err.Error(); got != want {
		t.Errorf("Error = %q, want %q", got, want)
	}
}

func TestEnsureStateDirCreateFail(t *testing.T) {
	// Try to create a dir inside a file (should fail)
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "notadir")
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.Close()
	err = ensureStateDir(filepath.Join(filePath, "subdir"))
	if err == nil {
		t.Fatal("ensureStateDir inside file: want error, got nil")
	}
}

func TestLoadEnsureStateDirFail(t *testing.T) {
	// Create a file to use as state dir parent
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "notadir")
	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.Close()
	_, err = Load("v0.1.0", "", filepath.Join(filePath, "subdir"))
	if err == nil {
		t.Fatal("Load with invalid state dir: want error, got nil")
	}
}
