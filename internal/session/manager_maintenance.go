// Manager maintenance: tick, persistLocked, logoutLocked, credential logging.
package session

import (
	"context"
	"time"

	"gateway/internal/cdp"
)

func (m *Manager) tick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.refreshToken == "" {
		return
	}
	if m.now().Add(refreshAhead).After(m.tokenExpiry) {
		if err := m.refreshLocked(context.Background()); err != nil {
			m.logger.Warn("proactive refresh failed", "err", err)
		}
	}
	if m.tws != nil && m.now().Add(twsAhead).After(m.tws.ValidUntil) {
		if _, err := m.ensureTWSLocked(context.Background()); err != nil {
			m.logger.Warn("proactive TWS renew failed", "err", err)
		}
	}
}

func (m *Manager) persistLocked() error {
	if m.refreshToken == "" && m.userID != "" {
		return nil
	}
	return m.store.Save(&Data{
		UserID:       m.userID,
		Email:        m.email,
		EVMAddress:   m.evmAddress,
		RefreshToken: m.refreshToken,
	})
}

func (m *Manager) logoutLocked() {
	m.refreshToken = ""
	m.accessToken = ""
	m.tokenExpiry = time.Time{}
	m.dropTWSLocked()
	_ = m.store.Clear()
}

// dropTWSLocked wipes the signing secret in place and releases it. The scalar
// buffer belongs to cdp (WalletSecret.Wipe), so those bytes really are
// overwritten; transient copies inside a signature are not erasable in Go —
// see THREAT-MODEL T3 for the residual and the process-level mitigations.
func (m *Manager) dropTWSLocked() {
	if m.tws != nil {
		m.tws.Wipe()
		m.tws = nil
	}
}

// warnIfNotLocked reports a refused mlock exactly once per process: the key is
// still usable, so this is a Warn, never an error, and never per-renewal noise
// (a refused mlock stays refused for the life of the daemon).
func (m *Manager) warnIfNotLocked(ws *cdp.WalletSecret) {
	if ws.Locked() {
		return
	}
	m.mu.Lock()
	warned := m.mlockWarned
	m.mlockWarned = true
	m.mu.Unlock()
	if !warned {
		m.logger.Warn("session: TWS buffer not mlocked — raise RLIMIT_MEMLOCK for swap protection",
			"wallet_secret_id", ws.ID)
	}
}
