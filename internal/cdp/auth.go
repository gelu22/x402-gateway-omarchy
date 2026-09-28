// Package cdp implements the minimal CDP End User Accounts REST client.
//
// Split: auth.go (basic auth/verify), refresh.go (token refresh),
// tws.go (wallet secrets), mfa.go (MFA), enduser.go (EVM accounts).
package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

const APIHost = "api.cdp.coinbase.com"
const DefaultBaseURL = "https://" + APIHost + "/platform"
const refreshCookieName = "cdp_refresh_token"

type Client struct {
	BaseURL   string
	ProjectID string
	HTTP      *http.Client // jarred: keeps device cookies for init/verify
	plain     *http.Client // jarless: refresh must carry exactly one token source

	skew clockSkew // last local−CDP time difference (see clock.go)
}

func NewClient(projectID string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		BaseURL:   DefaultBaseURL,
		ProjectID: projectID,
		HTTP:      &http.Client{Timeout: 10 * time.Second, Jar: jar},
		plain:     &http.Client{Timeout: 10 * time.Second},
	}
}

type Session struct {
	UserID       string    `json:"user_id"`
	Email        string    `json:"email,omitempty"`
	AccessToken  string    `json:"-"`
	ValidUntil   time.Time `json:"valid_until"`
	RefreshToken string    `json:"-"`
}

type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	body := e.Body
	if len(body) > 400 {
		body = body[:400]
	}
	return fmt.Sprintf("cdp: status %d: %s", e.Status, body)
}

func IsAuthRejected(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Status == http.StatusUnauthorized
	}
	return false
}

// IsPolicyViolation reports whether err is a CDP Policy Engine rejection. The
// engine is the TEE-side ceiling on a single signature (SPIKE-POLICY-ENGINE), so
// this is a decision, not a fault: the daemon must surface it as its own code
// instead of the generic signer_error, or agents will retry a wall.
func IsPolicyViolation(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return strings.Contains(ae.Body, "policy_violation")
	}
	return false
}

func IsLimitExceeded(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		if ae.Status == http.StatusTooManyRequests {
			return true
		}
		return strings.Contains(ae.Body, "account_limit_exceeded")
	}
	return false
}

func (c *Client) PostJSON(ctx context.Context, path string, payload []byte, headers map[string]string) ([]byte, http.Header, error) {
	return c.do(ctx, c.HTTP, http.MethodPost, path, json.RawMessage(payload), headers)
}

func (c *Client) do(ctx context.Context, client *http.Client, method, path string, body any, extraHeaders map[string]string) ([]byte, http.Header, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("cdp: request failed: %w", err)
	}
	defer res.Body.Close()
	// Every response (success or error) carries the server clock; observing it
	// here keeps /status honest without an extra request (THREAT-MODEL T6).
	if d, perr := http.ParseTime(res.Header.Get("Date")); perr == nil {
		c.skew.observe(d, time.Now())
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, res.Header, err
	}
	if res.StatusCode >= 300 {
		return raw, res.Header, &apiError{Status: res.StatusCode, Body: string(raw)}
	}
	return raw, res.Header, nil
}

type initiateResponse struct {
	Message string `json:"message"`
	FlowID  string `json:"flowId"`
}

func (c *Client) InitiateEmailOTP(ctx context.Context, email string) (flowID, message string, err error) {
	path := "/v2/embedded-wallet-api/projects/" + c.ProjectID + "/auth/init"
	raw, _, err := c.do(ctx, c.HTTP, http.MethodPost, path, map[string]string{"type": "email", "email": email}, nil)
	if err != nil {
		return "", "", err
	}
	var out initiateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("cdp: init decode: %w (%s)", err, truncate(raw))
	}
	return out.FlowID, out.Message, nil
}

type endUser struct {
	UserID                string `json:"userId"`
	AuthenticationMethods []struct {
		Type  string `json:"type"`
		Email string `json:"email,omitempty"`
	} `json:"authenticationMethods"`
}

func (e endUser) email() string {
	for _, m := range e.AuthenticationMethods {
		if m.Email != "" {
			return m.Email
		}
	}
	return ""
}
