package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/skip2/go-qrcode"
)

// MfaMethods mirrors the documented mfaMethods on the end-user object
// (totp/sms enrollments with enrolledAt timestamps).
type MfaMethods struct {
	Totp                 *MfaEnrollment `json:"totp,omitempty"`
	Sms                  *MfaEnrollment `json:"sms,omitempty"`
	EnrollmentPromptedAt string         `json:"enrollmentPromptedAt,omitempty"`
	// LastVerificationCompletedAt is CDP's own record of the last completed
	// verification (RFC3339). It is the only freshness proof a local caller
	// cannot forge without the TOTP, so the sudo gate reads it directly.
	LastVerificationCompletedAt string `json:"lastVerificationCompletedAt,omitempty"`
}

// MfaEnrollment is a single enrolled method.
type MfaEnrollment struct {
	EnrolledAt string `json:"enrolledAt"`
}

// Enrolled reports whether any MFA method is enrolled.
func (m *MfaMethods) Enrolled() bool {
	return m != nil && (m.Totp != nil || m.Sms != nil)
}

// Methods returns enrolled method names ("totp", "sms"). Nil-safe.
func (m *MfaMethods) Methods() []string {
	if m == nil {
		return nil
	}
	var out []string
	if m.Totp != nil {
		out = append(out, "totp")
	}
	if m.Sms != nil {
		out = append(out, "sms")
	}
	return out
}

func mfaPath(userID, op, method, step string) string {
	return "/v2/embedded-wallet-api/end-users/" + userID + "/mfa/" + op + "/" + method + "/" + step
}

func (c *Client) mfaPost(ctx context.Context, userID, accessToken, op, method, step string, body any) ([]byte, error) {
	raw, _, err := c.do(ctx, c.HTTP, http.MethodPost, mfaPath(userID, op, method, step), body, map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	return raw, err
}

// InitiateMfaEnrollment starts enrollment for method ("totp"). Returns the
// otpauth:// URL (QR) and the base32 secret (manual entry). The secret must
// be verified within 5 minutes. Uses the /initiate variant (the one
// @coinbase/cdp-core calls; verified live, S7).
func (c *Client) InitiateMfaEnrollment(ctx context.Context, userID, accessToken, method string) (authURL, secret string, err error) {
	raw, err := c.mfaPost(ctx, userID, accessToken, "enroll", method, "initiate", map[string]string{"type": method})
	if err != nil {
		return "", "", err
	}
	var out struct {
		AuthURL string `json:"authUrl"`
		Secret  string `json:"secret"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("cdp: mfa enroll init decode: %w (%s)", err, truncate(raw))
	}
	if out.AuthURL == "" || out.Secret == "" {
		return "", "", fmt.Errorf("cdp: mfa enroll init incomplete (%s)", truncate(raw))
	}
	return out.AuthURL, out.Secret, nil
}

// SubmitMfaEnrollment completes enrollment with the 6-digit code. On success
// the method is PERSISTED on the account.
func (c *Client) SubmitMfaEnrollment(ctx context.Context, userID, accessToken, method, code string) error {
	_, err := c.mfaPost(ctx, userID, accessToken, "enroll", method, "submit", map[string]string{"mfaCode": code})
	return err
}

// InitiateMfaVerification prepares a verification session (TOTP answers {}).
func (c *Client) InitiateMfaVerification(ctx context.Context, userID, accessToken, method string) error {
	_, err := c.mfaPost(ctx, userID, accessToken, "verify", method, "init", map[string]string{})
	return err
}

// SubmitMfaVerification completes verification with the 6-digit code.
func (c *Client) SubmitMfaVerification(ctx context.Context, userID, accessToken, method, code string) error {
	_, err := c.mfaPost(ctx, userID, accessToken, "verify", method, "submit", map[string]string{"mfaCode": code})
	return err
}

// GetMfaMethods reads the enrolled methods from the end-user record.
func (c *Client) GetMfaMethods(ctx context.Context, userID, accessToken string) (*MfaMethods, error) {
	raw, _, err := c.do(ctx, c.HTTP, http.MethodGet,
		"/v2/embedded-wallet-api/end-users/"+userID, nil,
		map[string]string{"Authorization": "Bearer " + accessToken})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		MfaMethods *MfaMethods `json:"mfaMethods"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("cdp: mfa status decode: %w (%s)", err, truncate(raw))
	}
	if parsed.MfaMethods == nil {
		return &MfaMethods{}, nil
	}
	return parsed.MfaMethods, nil
}

// IsMFARequired reports whether err is a CDP mfa_required rejection — the
// end user must complete MFA verification before signing. Unlike a 401 it
// does NOT end the session; unlike a limit it needs interaction, not time.
func IsMFARequired(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return strings.Contains(ae.Body, "mfa_required")
	}
	return false
}

// QRDataURI renders text (an otpauth:// URL) as a PNG data URI for the panel
// to display with a plain Image element (no QR logic in QML).
func QRDataURI(text string) (string, error) {
	png, err := qrcode.Encode(text, qrcode.Medium, 256)
	if err != nil {
		return "", fmt.Errorf("cdp: qr encode: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
