// MFA methods for session.Manager.
package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gateway/internal/cdp"
)

// validMfaCode reports whether code looks like a CDP 6-digit MFA code.
func validMfaCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for i := 0; i < 6; i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// MfaEnrollInit starts TOTP enrollment. Returns the otpauth URL, the secret
// (manual entry) and a QR data URI for the panel. Verify within 5 minutes.
func (m *Manager) MfaEnrollInit(ctx context.Context) (otpauthURL, secret, qrDataURI string, err error) {
	token, err := m.AccessToken()
	if err != nil {
		return "", "", "", err
	}
	authURL, secret, err := m.client.InitiateMfaEnrollment(ctx, m.UserID(), token, "totp")
	if err != nil {
		return "", "", "", err
	}
	qr, err := cdp.QRDataURI(authURL)
	if err != nil {
		return "", "", "", err
	}
	return authURL, secret, qr, nil
}

// MfaEnrollSubmit completes enrollment. On success the MFA cache is set
// directly (CDP confirmed persistence with 2xx).
func (m *Manager) MfaEnrollSubmit(ctx context.Context, code string) error {
	if !validMfaCode(code) {
		return fmt.Errorf("session: mfa code must be 6 digits")
	}
	token, err := m.AccessToken()
	if err != nil {
		return err
	}
	if err := m.client.SubmitMfaEnrollment(ctx, m.UserID(), token, "totp", code); err != nil {
		return err
	}
	m.mu.Lock()
	m.mfaEnrolled = true
	m.mfaMethod = "totp"
	m.mfaCheckedAt = m.now()
	m.mu.Unlock()
	return nil
}

// MfaVerifyInit prepares a verification session.
func (m *Manager) MfaVerifyInit(ctx context.Context) error {
	token, err := m.AccessToken()
	if err != nil {
		return err
	}
	return m.client.InitiateMfaVerification(ctx, m.UserID(), token, "totp")
}

// MfaVerifySubmit completes verification with the 6-digit code.
func (m *Manager) MfaVerifySubmit(ctx context.Context, code string) error {
	if !validMfaCode(code) {
		return fmt.Errorf("session: mfa code must be 6 digits")
	}
	token, err := m.AccessToken()
	if err != nil {
		return err
	}
	return m.client.SubmitMfaVerification(ctx, m.UserID(), token, "totp", code)
}

// MfaStatus returns the cached enrollment (CDP-authoritative, refreshed on
// auth events and at most every mfaCacheTTL). On error it keeps the last
// known value (fail-safe display, never flaps to false).
func (m *Manager) MfaStatus(ctx context.Context) (enrolled bool, method string) {
	m.mu.Lock()
	if m.refreshToken == "" {
		m.mu.Unlock()
		return false, ""
	}
	if !m.mfaCheckedAt.IsZero() && m.now().Before(m.mfaCheckedAt.Add(mfaCacheTTL)) {
		enrolled, method = m.mfaEnrolled, m.mfaMethod
		m.mu.Unlock()
		return enrolled, method
	}
	m.mu.Unlock()

	token, err := m.AccessToken()
	if err != nil {
		return m.cachedMFA()
	}
	methods, err := m.client.GetMfaMethods(ctx, m.UserID(), token)
	if err != nil {
		return m.cachedMFA()
	}
	m.mu.Lock()
	m.mfaEnrolled = methods.Enrolled()
	m.mfaMethod = strings.Join(methods.Methods(), ",")
	m.mfaCheckedAt = m.now()
	enrolled, method = m.mfaEnrolled, m.mfaMethod
	m.mu.Unlock()
	return enrolled, method
}

func (m *Manager) cachedMFA() (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mfaEnrolled, m.mfaMethod
}

// mfaClockSkew bounds how far ahead of local time a CDP verification timestamp
// may be before it counts as bogus. NTP skew is real (THREAT-MODEL T6: ±5 min),
// so a small allowance prevents the gate from locking out a user whose CDP
// clock runs fast, while a far-future timestamp is rejected instead of counting
// as "fresh forever".
const mfaClockSkew = time.Minute

// MfaVerifiedWithin reports whether a CDP MFA verification completed within d.
//
//   - (false, false, nil) — MFA is not enrolled (ADR D8: optional); the sudo
//     gate is inert, there is nothing to prove.
//   - (true, true, nil)   — enrolled and verified within d.
//   - (false, true, nil)  — enrolled, verification older than d.
//   - error               — cannot confirm (no session / no timestamp / bad
//     timestamp / CDP read failure): the caller must fail closed.
//
// No session is an *error*, not "inert": POST /pair/logout is unauthenticated on
// the socket, so treating a dropped session as "MFA off" would let a local
// process disarm the gate (logout, then raise the cap) and the raised cap would
// survive the next pairing.
//
// The check is deliberately uncached: MfaStatus serves the UI with a 5-minute
// cache, which is useless as a freshness proof.
func (m *Manager) MfaVerifiedWithin(ctx context.Context, d time.Duration) (verified bool, enrolled bool, stamp string, err error) {
	m.mu.Lock()
	signedIn := m.refreshToken != ""
	m.mu.Unlock()
	if !signedIn {
		return false, false, "", errors.New("session: not signed in, cannot confirm MFA freshness")
	}
	token, err := m.AccessToken()
	if err != nil {
		return false, false, "", err
	}
	methods, err := m.client.GetMfaMethods(ctx, m.UserID(), token)
	if err != nil {
		return false, false, "", err
	}
	if !methods.Enrolled() {
		return false, false, "", nil
	}
	at, err := time.Parse(time.RFC3339, methods.LastVerificationCompletedAt)
	if err != nil {
		return false, true, "", fmt.Errorf("session: mfa verification timestamp: %w", err)
	}
	m.mu.Lock()
	now := m.now()
	m.mu.Unlock()
	age := now.Sub(at)
	if age > d || age < -mfaClockSkew {
		return false, true, "", nil
	}
	// Fresh: hand back the CDP stamp so the caller can spend this specific
	// verification once (46.7). Returning it is not consuming it.
	return true, true, methods.LastVerificationCompletedAt, nil
}
