// Package session manages the end-user auth lifecycle: pairing, TWS, token
// refresh, MFA, and the gateway.Signer interface.
//
// Split: manager.go (state, lifecycle), auth.go (pairing, TWS, refresh),
// mfa.go (MFA methods).
package session

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"gateway/internal/cdp"
)

// State is the coarse session state exposed via /status.
type State string

const (
	StateLoggedOut State = "logged_out"
	StateActive    State = "active"
)

const (
	refreshAhead     = 3 * time.Minute // refresh when access token expires within 3 min
	twsAhead         = 2 * time.Minute // rotate TWS when validUntil within 2 min
	twsTTL           = 10 * time.Minute
	tickEvery        = time.Minute
	twsLimitCooldown = 5 * time.Minute
	mfaCacheTTL      = 5 * time.Minute
)

var (
	ErrNotSignedIn = errors.New("session: not signed in")
	ErrTWSLimit    = errors.New("session: tws limit exceeded")
)

// Manager implements gateway.Signer with proactive lifecycle management.
type Manager struct {
	client *cdp.Client
	store  *Store
	logger *slog.Logger
	now    func() time.Time

	OnSignedIn func(accessToken string)

	mu              sync.Mutex
	userID          string
	email           string
	evmAddress      string
	refreshToken    string
	accessToken     string
	tokenExpiry     time.Time
	tws             *cdp.WalletSecret
	twsLimitedUntil time.Time
	mlockWarned     bool
	mfaEnrolled     bool
	mfaMethod       string
	mfaCheckedAt    time.Time
	resumedOnce     bool
}

// New creates the manager and attempts to resume a stored session.
func New(client *cdp.Client, store *Store, logger *slog.Logger) (*Manager, error) {
	m := &Manager{client: client, store: store, logger: logger, now: time.Now}
	data, err := store.Load()
	if err != nil {
		if !errors.Is(err, ErrNotFound) && logger != nil {
			logger.Warn("session: starting logged out", "reason", err.Error())
		}
		return m, nil
	}
	m.userID = data.UserID
	m.email = data.Email
	m.evmAddress = data.EVMAddress
	m.refreshToken = data.RefreshToken
	return m, nil
}

func (m *Manager) SetNow(f func() time.Time) { m.now = f }

// Run blocks running the maintenance loop until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.tick()
		}
	}
}

// Status returns (state, email, evmAddress).
func (m *Manager) Status() (State, string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refreshToken == "" {
		return StateLoggedOut, m.email, m.evmAddress
	}
	return StateActive, m.email, m.evmAddress
}

// PairState returns (state, email, walletAddress) for the socket /pair endpoints.
func (m *Manager) PairState() (state, email, walletAddress string) {
	s, e, a := m.Status()
	return string(s), e, a
}

// State returns the coarse state string.
func (m *Manager) State() string {
	s, _, _ := m.Status()
	return string(s)
}

// Persist saves the current session to disk (refresh token + identity).
func (m *Manager) Persist() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.persistLocked()
}

// Logout idempotently ends the session: clears tokens, TWS, and removes the
// session file from disk. Repeated calls succeed without error.
func (m *Manager) Logout() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logoutLocked()
	return nil
}

// --- gateway.Signer methods ---

func (m *Manager) Address() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.evmAddress
}

func (m *Manager) UserID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.userID
}

func (m *Manager) AccessToken() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refreshToken == "" {
		return "", ErrNotSignedIn
	}
	if m.accessToken == "" || !m.now().Add(refreshAhead).Before(m.tokenExpiry) {
		if err := m.refreshLocked(context.Background()); err != nil {
			return "", err
		}
	}
	return m.accessToken, nil
}

func (m *Manager) WalletSecret() (*cdp.WalletSecret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refreshToken == "" {
		return nil, ErrNotSignedIn
	}
	if m.tws == nil || !m.now().Add(twsAhead).Before(m.tws.ValidUntil) {
		if _, err := m.ensureTWSLocked(context.Background()); err != nil {
			return nil, err
		}
	}
	return m.tws, nil
}

// DropTWS wipes and releases the signing secret. Shutdown calls this
// explicitly because os.Exit skips deferred cleanup — the key must not sit in
// memory while the process is torn down.
func (m *Manager) DropTWS() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropTWSLocked()
}
