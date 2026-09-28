// Token refresh logic for CDP auth.
package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type verifyResponse struct {
	EndUser      endUser `json:"endUser"`
	Message      string  `json:"message"`
	AccessToken  string  `json:"accessToken"`
	ValidUntil   string  `json:"validUntil"`
	RefreshToken string  `json:"refreshToken"`
}

func (c *Client) VerifyEmailOTP(ctx context.Context, flowID, otp string) (*Session, error) {
	return c.verifyWith(ctx, c.HTTP, "/v2/embedded-wallet-api/projects/"+c.ProjectID+"/auth/verify/email", map[string]string{
		"flowId": flowID,
		"otp":    otp,
	})
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Session, error) {
	return c.verifyWith(ctx, c.plain, "/v2/embedded-wallet-api/projects/"+c.ProjectID+"/auth/refresh", map[string]string{
		"grantType":    "refresh_token",
		"refreshToken": refreshToken,
	})
}

func (c *Client) verifyWith(ctx context.Context, client *http.Client, path string, body map[string]string) (*Session, error) {
	raw, header, err := c.do(ctx, client, http.MethodPost, path, body, nil)
	if err != nil {
		return nil, err
	}
	var out verifyResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("cdp: verify decode: %w (%s)", err, truncate(raw))
	}
	rt := out.RefreshToken
	if rt == "" {
		for _, ck := range header.Values("Set-Cookie") {
			if v, ok := cookieValue(ck, refreshCookieName); ok {
				rt = v
				break
			}
		}
	}
	if rt == "" {
		return nil, fmt.Errorf("cdp: no refresh token in body or %s cookie (%s)", refreshCookieName, truncate(raw))
	}
	validUntil, err := time.Parse(time.RFC3339, out.ValidUntil)
	if err != nil {
		return nil, fmt.Errorf("cdp: validUntil parse: %w", err)
	}
	return &Session{
		UserID:       out.EndUser.UserID,
		Email:        out.EndUser.email(),
		AccessToken:  out.AccessToken,
		ValidUntil:   validUntil,
		RefreshToken: rt,
	}, nil
}

func cookieValue(setCookie, name string) (string, bool) {
	for _, part := range strings.Split(setCookie, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 && kv[0] == name {
			return kv[1], true
		}
	}
	return "", false
}

func truncate(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
