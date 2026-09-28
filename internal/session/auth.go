// Auth, pairing, and TWS management.
package session

import (
	"context"
	"fmt"
	"time"

	"gateway/internal/cdp"
)

// InitPairing starts the email OTP flow (wizard step 1).
func (m *Manager) InitPairing(ctx context.Context, email string) (flowID, message string, err error) {
	return m.client.InitiateEmailOTP(ctx, email)
}

// VerifyPairing completes onboarding (wizard step 2): verify OTP, provision
// the EVM account when missing, register the first TWS and persist identity.
func (m *Manager) VerifyPairing(ctx context.Context, flowID, otp string) error {
	sess, err := m.client.VerifyEmailOTP(ctx, flowID, otp)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.userID = sess.UserID
	m.email = sess.Email
	m.accessToken = sess.AccessToken
	m.tokenExpiry = sess.ValidUntil
	m.refreshToken = sess.RefreshToken
	m.mu.Unlock()

	// TWS first: creating the EVM account requires X-Wallet-Auth.
	if _, err := m.ensureTWSLocked(ctx); err != nil {
		return err
	}
	if err := m.ensureEVM(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.persistLocked(); err != nil {
		return err
	}
	if m.OnSignedIn != nil && m.accessToken != "" {
		m.OnSignedIn(m.accessToken)
	}
	return nil
}

// ensureEVM provisions the end user's EVM account when missing. Caller must
// NOT hold m.mu (CDP calls serialize through AccessToken/WalletSecret locks).
func (m *Manager) ensureEVM(ctx context.Context) error {
	token, err := m.AccessToken()
	if err != nil {
		return err
	}
	ws, err := m.WalletSecret()
	if err != nil {
		return err
	}
	accounts, err := m.client.GetEndUser(ctx, m.UserID(), token)
	if err != nil {
		return fmt.Errorf("session: end user: %w", err)
	}
	if len(accounts) > 0 {
		m.mu.Lock()
		m.evmAddress = accounts[0].Address
		m.mu.Unlock()
		return nil
	}
	account, err := m.client.CreateEvmAccount(ctx, m.UserID(), token, ws)
	if err != nil {
		return fmt.Errorf("session: create evm: %w", err)
	}
	m.mu.Lock()
	m.evmAddress = account.Address
	m.mu.Unlock()
	return nil
}

// refreshLocked exchanges the current refresh token; rotates it in memory and
// persists immediately (the previous token is dead after rotation).
func (m *Manager) refreshLocked(ctx context.Context) error {
	sess, err := m.client.Refresh(ctx, m.refreshToken)
	if err != nil {
		if cdp.IsAuthRejected(err) {
			m.logoutLocked()
			return fmt.Errorf("session: refresh rejected (%w), logged out", ErrLoggedOut)
		}
		return fmt.Errorf("session: refresh: %w", err)
	}
	m.accessToken = sess.AccessToken
	m.tokenExpiry = sess.ValidUntil
	m.refreshToken = sess.RefreshToken
	firstResume := m.evmAddress != "" && !m.resumedOnce
	m.resumedOnce = true
	if err := m.persistLocked(); err != nil {
		return err
	}
	if firstResume && m.OnSignedIn != nil {
		m.OnSignedIn(m.accessToken)
	}
	return nil
}

// ensureTWSLocked returns a valid TWS: it creates one on bootstrap and
// renews the SAME walletSecretId+key afterwards (S6: CDP-compliant renewal;
// supersedes the rotate-with-new-ID behavior of 002.2, which accumulated
// identities toward account limits).
func (m *Manager) ensureTWSLocked(ctx context.Context) (*cdp.WalletSecret, error) {
	// Circuit breaker (fail-closed, no retry-storm): after a CDP limit
	// rejection, fail fast until the cooldown passes.
	if !m.twsLimitedUntil.IsZero() && m.now().Before(m.twsLimitedUntil) {
		return nil, fmt.Errorf("session: tws limited until %s: %w", m.twsLimitedUntil.Format(time.RFC3339), ErrTWSLimit)
	}
	if m.accessToken == "" || !m.now().Add(time.Minute).Before(m.tokenExpiry) {
		if err := m.refreshLocked(ctx); err != nil {
			return nil, err
		}
	}
	if m.tws == nil {
		ws, err := m.client.CreateWalletSecret(ctx, m.userID, m.accessToken, m.now().Add(twsTTL), "")
		if err != nil {
			return nil, m.limitOr("create", err)
		}
		m.tws = ws
		m.twsLimitedUntil = time.Time{}
		m.warnIfNotLocked(ws)
		return ws, nil
	}
	ws, err := m.client.RenewWalletSecret(ctx, m.userID, m.accessToken, m.tws, m.now().Add(twsTTL))
	if err != nil {
		if cdp.IsAuthRejected(err) {
			m.logoutLocked()
			return nil, fmt.Errorf("session: tws renew rejected (%w), logged out", ErrLoggedOut)
		}
		return nil, m.limitOr("renew", err)
	}
	// The old key is superseded only now that the new one is certain: wipe it
	// so renewed buffers do not accumulate in (mlocked) memory. On renew
	// failure m.tws — and the session — stay untouched (fail-closed).
	old := m.tws
	m.tws = ws
	if old != nil {
		old.Wipe()
	}
	m.twsLimitedUntil = time.Time{}
	m.warnIfNotLocked(ws)
	return ws, nil
}

// limitOr maps a CDP limit rejection to ErrTWSLimit and arms the circuit
// breaker; all other errors pass through wrapped but unlimited.
func (m *Manager) limitOr(op string, err error) error {
	if cdp.IsLimitExceeded(err) {
		m.twsLimitedUntil = m.now().Add(twsLimitCooldown)
		return fmt.Errorf("session: tws %s limited (%v): %w", op, err, ErrTWSLimit)
	}
	return fmt.Errorf("session: tws %s: %w", op, err)
}
