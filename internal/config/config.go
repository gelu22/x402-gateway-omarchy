// Package config loads the gateway daemon configuration from environment
// variables and CLI flags. Missing optional values fall back to XDG defaults;
// invalid values fail fast with actionable errors (fail-closed).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is the daemon runtime configuration.
type Config struct {
	SocketPath string // unix socket path (contract: CONTRACTS.md §1)
	StateDir   string // persistent state dir, perms 0700
	Network    string // CAIP-2 network for payments
	Address    string // wallet EVM address (balance queries; may be empty)
	ProjectID  string // CDP project ID; empty = auth disabled (WARN)
	CDPBaseURL string // CDP API base URL (default: https://api.cdp.coinbase.com/platform)
	Version    string // build version
}

const (
	defaultNetwork = "eip155:84532" // Base Sepolia (THREAT-MODEL T1 allowlist)
	stateDirPerms  = 0o700

	// defaultProjectID is the product's dedicated CDP project (native email
	// OTP enabled). Baked in so the daemon works without any env setup;
	// CDP_PROJECT_ID env overrides for testing.
	defaultProjectID = "387b5ae3-162d-4d44-b99b-0fc8e4988623"
)

// Load resolves configuration from flags → env → defaults, creates StateDir
// with 0700 when missing and validates values.
func Load(version, socketPathFlag, stateDirFlag string) (*Config, error) {
	cfg := &Config{
		Version:    version,
		ProjectID:  firstNonEmpty(strings.TrimSpace(os.Getenv("CDP_PROJECT_ID")), defaultProjectID),
		Network:    envOr("GATEWAY_NETWORK", defaultNetwork),
		CDPBaseURL: envOr("CDP_BASE_URL", ""),
	}
	cfg.Address = strings.TrimSpace(os.Getenv("GATEWAY_EVM_ADDRESS"))
	cfg.SocketPath = firstNonEmpty(socketPathFlag, os.Getenv("GATEWAY_SOCKET_PATH"),
		filepath.Join(stateRoot(), "x402-gateway", "gw.sock"))
	stateDir := firstNonEmpty(stateDirFlag, os.Getenv("GATEWAY_STATE_DIR"),
		filepath.Join(stateRoot(), "x402-gateway"))

	if !strings.HasPrefix(cfg.Network, "eip155:") {
		return nil, fmt.Errorf("config: unsupported network %q (want CAIP-2 eip155:<chain>)", cfg.Network)
	}
	if err := ensureStateDir(stateDir); err != nil {
		return nil, err
	}
	cfg.StateDir = stateDir
	return cfg, nil
}

// stateRoot returns XDG_STATE_DIR or ~/.local/state.
func stateRoot() string {
	if x := os.Getenv("XDG_STATE_DIR"); x != "" {
		return x
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".local", "state")
}

// EnsureStateDir creates dir with 0700 if missing (THREAT-MODEL T2).
func ensureStateDir(dir string) error {
	if err := os.MkdirAll(dir, stateDirPerms); err != nil { // #nosec G703 -- dir from local CLI flags/env (same-user trust); no remote input reaches it
		return fmt.Errorf("config: state dir %s: %w", dir, err)
	}
	info, err := os.Stat(dir) // #nosec G703 -- same user-owned dir as above
	if err != nil {
		return err
	}
	if perm := info.Mode().Perm(); perm != stateDirPerms {
		if err := os.Chmod(dir, stateDirPerms); err != nil { // #nosec G703 -- same user-owned dir as above
			return fmt.Errorf("config: chmod %s: %w", dir, err)
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
