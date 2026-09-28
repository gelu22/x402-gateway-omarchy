// Package session manages the daemon's long-lived CDP credentials:
// proactive access-token refresh, TWS rotation and durable storage of the
// rotating refresh token (the only secret allowed on disk, 0600).
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Store persists identity + the rotating refresh token. Access tokens and
// TWS private keys live only in process memory (THREAT-MODEL T3).
type Store struct {
	path string
}

// Data is the on-disk session record.
type Data struct {
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	EVMAddress   string `json:"evm_address"`
	RefreshToken string `json:"refresh_token"`
}

var ErrLoggedOut = errors.New("session: logged out")

// ErrNotFound is returned by Load when no session exists yet.
var ErrNotFound = errors.New("session: no stored session")

func NewStore(stateDir string) *Store {
	return &Store{path: filepath.Join(stateDir, "session.json")}
}

// Load reads the stored session; ErrNotFound when absent.
func (s *Store) Load() (*Data, error) {
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("session: read: %w", err)
	}
	var d Data
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("session: corrupt %s: %w", s.path, err)
	}
	if d.UserID == "" || d.RefreshToken == "" {
		return nil, ErrLoggedOut
	}
	return &d, nil
}

// Save writes atomically (tmp+rename) with 0600. Must be called immediately
// after every refresh-token rotation — the previous token is dead by then.
func (s *Store) Save(d *Data) error {
	raw, err := json.MarshalIndent(d, "", "  ") // #nosec G117 -- refresh token persisted by design (0600, THREAT-MODEL); required across restarts
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("session: write: %w", err)
	}
	return os.Rename(tmp, s.path)
}

// Clear removes the stored session (logout / revoked refresh token).
func (s *Store) Clear() error {
	err := os.Remove(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
