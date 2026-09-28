package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	contractProjectID  = "387b5ae3-0000-0000-0000-000000000000"
	contractUserID     = "00000000-0000-4000-8000-000000000000"
	contractEmail      = "operator@example.com"
	contractFlowID     = "flow-00000000-0000-4000-8000-000000000000"
	contractAccess     = "access-contract-1"
	contractRefresh    = "refresh-contract-1"
	contractSecretID   = "11111111-2222-4333-8444-555555555555"
	contractValidUntil = "2026-08-23T12:15:00Z"
)

// contractCDP emulates the CDP surface with fixed fixtures. Every response
// field the daemon reads is asserted by value, so a silent CDP rename
// (e.g. flowId → flow_id) turns these tests red. No network leaves the test.
func contractCDP(t *testing.T, cookieRefresh bool) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/auth/init"):
			writeContractJSON(w, http.StatusOK, map[string]any{
				"message": "OTP sent",
				"flowId":  contractFlowID,
			})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/auth/verify/email"):
			body := map[string]any{
				"endUser": map[string]any{
					"userId": contractUserID,
					"authenticationMethods": []map[string]any{
						{"type": "email", "email": contractEmail},
					},
				},
				"message":     "verified",
				"accessToken": contractAccess,
				"validUntil":  contractValidUntil,
			}
			if cookieRefresh {
				w.Header().Add("Set-Cookie", refreshCookieName+"="+contractRefresh+"; Path=/; HttpOnly")
			} else {
				body["refreshToken"] = contractRefresh
			}
			writeContractJSON(w, http.StatusOK, body)
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/auth/refresh"):
			writeContractJSON(w, http.StatusOK, map[string]any{
				"endUser": map[string]any{
					"userId": contractUserID,
					"authenticationMethods": []map[string]any{
						{"type": "email", "email": contractEmail},
					},
				},
				"message":      "refreshed",
				"accessToken":  "access-contract-2",
				"validUntil":   contractValidUntil,
				"refreshToken": "refresh-contract-2",
			})
		case r.Method == http.MethodPut && strings.HasSuffix(p, "/wallet-secrets"):
			if r.Header.Get("Authorization") == "" || r.Header.Get("X-Wallet-Auth") == "" {
				writeContractJSON(w, http.StatusUnauthorized, map[string]string{"errorType": "unauthorized"})
				return
			}
			var in struct {
				WalletSecretID string `json:"walletSecretId"`
				PublicKey      string `json:"publicKey"`
				ValidUntil     string `json:"validUntil"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.WalletSecretID == "" || in.PublicKey == "" || in.ValidUntil == "" {
				writeContractJSON(w, http.StatusBadRequest, map[string]string{"errorType": "invalid_request"})
				return
			}
			writeContractJSON(w, http.StatusOK, map[string]any{
				"walletSecretId": in.WalletSecretID,
				"validUntil":     in.ValidUntil,
			})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/evm"):
			if r.Header.Get("Authorization") == "" || r.Header.Get("X-Wallet-Auth") == "" {
				writeContractJSON(w, http.StatusUnauthorized, map[string]string{"errorType": "unauthorized"})
				return
			}
			writeContractJSON(w, http.StatusOK, map[string][]map[string]string{
				"evmAccountObjects": {{"address": "0x1234567890abcdef1234567890abcdef12345678", "createdAt": "2025-01-01T00:00:00Z"}},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/v2/embedded-wallet-api/end-users/"):
			// Check query/path for legacy format test
			if strings.Contains(p, "legacy") {
				writeContractJSON(w, http.StatusOK, map[string]any{
					"userId":                contractUserID,
					"evmAccountObjects":     []map[string]string{},
					"evmAccounts":           []string{"0xlegacy1", "0xlegacy2"},
					"authenticationMethods": []map[string]string{{"type": "email", "email": contractEmail}},
				})
			} else if strings.Contains(p, "decode_error") {
				_, _ = w.Write([]byte(`{invalid json`))
			} else if strings.Contains(p, "mfa_decode_error") {
				_, _ = w.Write([]byte(`{invalid mfa json`))
			} else {
				writeContractJSON(w, http.StatusOK, map[string]any{
					"userId":                contractUserID,
					"evmAccountObjects":     []map[string]string{{"address": "0xabcdef", "createdAt": "2025-01-01T00:00:00Z"}},
					"evmAccounts":           []string{},
					"authenticationMethods": []map[string]string{{"type": "email", "email": contractEmail}},
				})
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClient(contractProjectID)
	c.BaseURL = srv.URL
	return c
}

func writeContractJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestContractInitiateEmailOTP(t *testing.T) {
	c := contractCDP(t, false)
	flowID, msg, err := c.InitiateEmailOTP(context.Background(), contractEmail)
	if err != nil {
		t.Fatal(err)
	}
	if flowID != contractFlowID {
		t.Fatalf("flowId = %q, want %q", flowID, contractFlowID)
	}
	if msg == "" {
		t.Fatal("message must be non-empty (surfaced in UI)")
	}
}

func TestContractVerifyEmailOTP(t *testing.T) {
	c := contractCDP(t, false)
	sess, err := c.VerifyEmailOTP(context.Background(), contractFlowID, "123456")
	if err != nil {
		t.Fatal(err)
	}
	if sess.UserID != contractUserID || sess.Email != contractEmail {
		t.Fatalf("identity user=%q email=%q", sess.UserID, sess.Email)
	}
	if sess.AccessToken != contractAccess || sess.RefreshToken != contractRefresh {
		t.Fatal("tokens not parsed")
	}
	if got := sess.ValidUntil.Format(time.RFC3339); got != contractValidUntil {
		t.Fatalf("validUntil = %q, want %q", got, contractValidUntil)
	}
}

func TestContractRefreshCookieFallback(t *testing.T) {
	c := contractCDP(t, true)
	sess, err := c.VerifyEmailOTP(context.Background(), contractFlowID, "123456")
	if err != nil {
		t.Fatal(err)
	}
	if sess.RefreshToken != contractRefresh {
		t.Fatalf("refreshToken = %q, want cookie value %q", sess.RefreshToken, contractRefresh)
	}
}

func TestContractRefresh(t *testing.T) {
	c := contractCDP(t, false)
	sess, err := c.Refresh(context.Background(), "refresh-contract-0")
	if err != nil {
		t.Fatal(err)
	}
	if sess.AccessToken != "access-contract-2" || sess.RefreshToken != "refresh-contract-2" {
		t.Fatalf("rotated tokens not parsed: access=%q refresh=%q", sess.AccessToken, sess.RefreshToken)
	}
}

func TestContractWalletSecrets(t *testing.T) {
	c := contractCDP(t, false)
	vu, _ := time.Parse(time.RFC3339, contractValidUntil)
	ws, err := c.CreateWalletSecret(context.Background(), contractUserID, contractAccess, vu, contractSecretID)
	if err != nil {
		t.Fatal(err)
	}
	if ws.ID != contractSecretID {
		t.Fatalf("walletSecretId = %q, want %q", ws.ID, contractSecretID)
	}
	if got := ws.ValidUntil.Format(time.RFC3339); got != contractValidUntil {
		t.Fatalf("validUntil = %q, want %q", got, contractValidUntil)
	}
	if !ws.HasKey() || len(ws.PublicSPKI) == 0 {
		t.Fatal("keypair must be retained client-side")
	}
}

func TestAPIErrorFormat(t *testing.T) {
	err := &apiError{Status: 500, Body: `{"error":"server_error"}`}
	got := err.Error()
	want := `cdp: status 500: {"error":"server_error"}`
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestAPIErrorTruncate(t *testing.T) {
	longBody := strings.Repeat("e", 500)
	err := &apiError{Status: 502, Body: longBody}
	got := err.Error()
	if len(got) > 450 {
		t.Errorf("Error() len = %d, expected ~450 (prefix + 400 char body)", len(got))
	}
	if !strings.Contains(got, "cdp: status 502:") {
		t.Errorf("Error() = %q, want contains 'cdp: status 502:'", got)
	}
}

func TestIsAuthRejected(t *testing.T) {
	if IsAuthRejected(nil) {
		t.Error("IsAuthRejected(nil) = true, want false")
	}
	if IsAuthRejected(fmt.Errorf("random")) {
		t.Error("IsAuthRejected(non-apiError) = true, want false")
	}
	if !IsAuthRejected(&apiError{Status: 401, Body: `{"error":"auth_rejected"}`}) {
		t.Error("IsAuthRejected(401) = false, want true")
	}
	if IsAuthRejected(&apiError{Status: 403, Body: `{"error":"forbidden"}`}) {
		t.Error("IsAuthRejected(403) = true, want false")
	}
	if IsAuthRejected(&apiError{Status: 500, Body: `{"error":"server_error"}`}) {
		t.Error("IsAuthRejected(500) = true, want false")
	}
}

func TestIsLimitExceeded(t *testing.T) {
	if IsLimitExceeded(nil) {
		t.Error("IsLimitExceeded(nil) = true, want false")
	}
	if IsLimitExceeded(fmt.Errorf("random")) {
		t.Error("IsLimitExceeded(non-apiError) = true, want false")
	}
	if !IsLimitExceeded(&apiError{Status: 429, Body: `{"error":"rate_limited"}`}) {
		t.Error("IsLimitExceeded(429) = false, want true")
	}
	if !IsLimitExceeded(&apiError{Status: 400, Body: `{"errorType":"account_limit_exceeded"}`}) {
		t.Error("IsLimitExceeded(body=account_limit_exceeded) = false, want true")
	}
	if IsLimitExceeded(&apiError{Status: 500, Body: `{"error":"server_error"}`}) {
		t.Error("IsLimitExceeded(500) = true, want false")
	}
}

func TestIsMFARequired(t *testing.T) {
	if IsMFARequired(nil) {
		t.Error("IsMFARequired(nil) = true, want false")
	}
	if IsMFARequired(fmt.Errorf("random")) {
		t.Error("IsMFARequired(non-apiError) = true, want false")
	}
	if !IsMFARequired(&apiError{Status: 403, Body: `{"errorType":"mfa_required"}`}) {
		t.Error("IsMFARequired(mfa_required) = false, want true")
	}
	if IsMFARequired(&apiError{Status: 401, Body: `{"error":"auth_rejected"}`}) {
		t.Error("IsMFARequired(401) = true, want false")
	}
	if IsMFARequired(&apiError{Status: 500, Body: `{"error":"server_error"}`}) {
		t.Error("IsMFARequired(500) = true, want false")
	}
}

func TestRandomID(t *testing.T) {
	id1, err := randomID()
	if err != nil {
		t.Fatalf("randomID: %v", err)
	}
	id2, err := randomID()
	if err != nil {
		t.Fatalf("randomID: %v", err)
	}
	if id1 == id2 {
		t.Error("randomID() produced same ID twice")
	}
	// UUID-shaped: 8-4-4-4-12
	parts := strings.Split(id1, "-")
	if len(parts) != 5 {
		t.Errorf("randomID() = %q, want UUID format (8-4-4-4-12)", id1)
	} else if len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		t.Errorf("randomID() = %q, parts lengths = %v, want 8-4-4-4-12", id1, []int{len(parts[0]), len(parts[1]), len(parts[2]), len(parts[3]), len(parts[4])})
	}
}

func TestHostOf(t *testing.T) {
	if got := hostOf("https://api.cdp.coinbase.com"); got != "api.cdp.coinbase.com" {
		t.Errorf("hostOf(https) = %q, want %q", got, "api.cdp.coinbase.com")
	}
	if got := hostOf("http://localhost:8080"); got != "localhost:8080" {
		t.Errorf("hostOf(local) = %q, want %q", got, "localhost:8080")
	}
	// Invalid URL (no scheme) — url.Parse succeeds but Host is empty
	if got := hostOf("not-a-url"); got != "" {
		t.Errorf("hostOf(invalid) = %q, want empty (no scheme)", got)
	}
}

func TestRenewWalletSecretNilWS(t *testing.T) {
	c := contractCDP(t, false)
	_, err := c.RenewWalletSecret(context.Background(), contractUserID, contractAccess, nil, time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("RenewWalletSecret(nil WS): want error, got nil")
	}
	if !strings.Contains(err.Error(), "missing wallet secret key") {
		t.Errorf("error = %q, want contains 'missing wallet secret key'", err.Error())
	}
}

func TestRenewWalletSecretNilPrivate(t *testing.T) {
	c := contractCDP(t, false)
	ws := &WalletSecret{ID: "test-id"}
	_, err := c.RenewWalletSecret(context.Background(), contractUserID, contractAccess, ws, time.Now().Add(time.Hour))
	if err == nil {
		t.Fatal("RenewWalletSecret(nil Private): want error, got nil")
	}
	if !strings.Contains(err.Error(), "missing wallet secret key") {
		t.Errorf("error = %q, want contains 'missing wallet secret key'", err.Error())
	}
}

func TestMfaMethodsNil(t *testing.T) {
	var m *MfaMethods
	if m.Enrolled() {
		t.Error("nil MfaMethods.Enrolled() = true, want false")
	}
	if got := m.Methods(); got != nil {
		t.Errorf("nil MfaMethods.Methods() = %v, want nil", got)
	}
}

func TestMfaMethodsEmpty(t *testing.T) {
	m := &MfaMethods{}
	if m.Enrolled() {
		t.Error("empty MfaMethods.Enrolled() = true, want false")
	}
	if got := m.Methods(); len(got) != 0 {
		t.Errorf("empty MfaMethods.Methods() = %v, want empty", got)
	}
}

func TestMfaMethodsTotpOnly(t *testing.T) {
	m := &MfaMethods{Totp: &MfaEnrollment{EnrolledAt: "2025-01-01T00:00:00Z"}}
	if !m.Enrolled() {
		t.Error("Totp MfaMethods.Enrolled() = false, want true")
	}
	methods := m.Methods()
	if len(methods) != 1 || methods[0] != "totp" {
		t.Errorf("MfaMethods.Methods() = %v, want [totp]", methods)
	}
}

func TestMfaMethodsBoth(t *testing.T) {
	m := &MfaMethods{
		Totp: &MfaEnrollment{EnrolledAt: "2025-01-01T00:00:00Z"},
		Sms:  &MfaEnrollment{EnrolledAt: "2025-01-02T00:00:00Z"},
	}
	if !m.Enrolled() {
		t.Error("Both MfaMethods.Enrolled() = false, want true")
	}
	methods := m.Methods()
	if len(methods) != 2 {
		t.Errorf("MfaMethods.Methods() len = %d, want 2", len(methods))
	}
}

func TestCreateEvmAccount(t *testing.T) {
	c := contractCDP(t, false)
	vu, _ := time.Parse(time.RFC3339, contractValidUntil)
	ws, err := c.CreateWalletSecret(context.Background(), contractUserID, contractAccess, vu, contractSecretID)
	if err != nil {
		t.Fatalf("CreateWalletSecret: %v", err)
	}
	acc, err := c.CreateEvmAccount(context.Background(), contractUserID, contractAccess, ws)
	if err != nil {
		t.Fatalf("CreateEvmAccount: %v", err)
	}
	if acc.Address != "0x1234567890abcdef1234567890abcdef12345678" {
		t.Errorf("address = %q, want %q", acc.Address, "0x1234567890abcdef1234567890abcdef12345678")
	}
}

func TestGetEndUserEvmAccountObjects(t *testing.T) {
	c := contractCDP(t, false)
	accounts, err := c.GetEndUser(context.Background(), contractUserID, contractAccess)
	if err != nil {
		t.Fatalf("GetEndUser: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("accounts len = %d, want 1", len(accounts))
	}
	if accounts[0].Address != "0xabcdef" {
		t.Errorf("address = %q, want %q", accounts[0].Address, "0xabcdef")
	}
}

func TestRenewWalletSecretSuccess(t *testing.T) {
	c := contractCDP(t, false)
	vu, _ := time.Parse(time.RFC3339, contractValidUntil)
	ws, err := c.CreateWalletSecret(context.Background(), contractUserID, contractAccess, vu, contractSecretID)
	if err != nil {
		t.Fatalf("CreateWalletSecret: %v", err)
	}
	newVu := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	renewed, err := c.RenewWalletSecret(context.Background(), contractUserID, contractAccess, ws, newVu)
	if err != nil {
		t.Fatalf("RenewWalletSecret: %v", err)
	}
	if renewed.ID != contractSecretID {
		t.Errorf("renewed.ID = %q, want %q", renewed.ID, contractSecretID)
	}
	if renewed.PublicSPKI == nil || len(renewed.PublicSPKI) == 0 {
		t.Error("renewed must retain PublicSPKI")
	}
	if !renewed.HasKey() {
		t.Error("renewed must retain the private key")
	}
}

func TestCDPTruncateShort(t *testing.T) {
	input := []byte("short")
	got := truncate(input)
	if got != "short" {
		t.Errorf("truncate(5 bytes) = %q, want %q", got, "short")
	}
}

func TestCDPTruncateLong(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := truncate([]byte(long))
	if len(got) != 203 {
		t.Errorf("truncate(300 bytes) len = %d, want 203 (200 + 3-byte …)", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate(300 bytes) = %q, want ends with '…'", got)
	}
}

func TestCookieValue(t *testing.T) {
	tests := []struct {
		setCookie string
		name      string
		want      string
		wantOk    bool
	}{
		{"cdp_refresh_token=tok123; Path=/; HttpOnly", "cdp_refresh_token", "tok123", true},
		{"cdp_refresh_token=tok123", "cdp_refresh_token", "tok123", true},
		{"other_token=tok456; Path=/", "cdp_refresh_token", "", false},
		{"", "cdp_refresh_token", "", false},
		{"cdp_refresh_token=; Path=/", "cdp_refresh_token", "", true},
	}
	for _, tt := range tests {
		got, ok := cookieValue(tt.setCookie, tt.name)
		if ok != tt.wantOk {
			t.Errorf("cookieValue(%q, %q) ok = %v, want %v", tt.setCookie, tt.name, ok, tt.wantOk)
		}
		if got != tt.want {
			t.Errorf("cookieValue(%q, %q) = %q, want %q", tt.setCookie, tt.name, got, tt.want)
		}
	}
}

func TestQRDataURIBadInput(t *testing.T) {
	// QR code with invalid text (empty should still work, but very large text may fail)
	large := strings.Repeat("x", 100000)
	_, err := QRDataURI(large)
	if err == nil {
		t.Error("QRDataURI(100k chars): want error, got nil")
	}
}

func TestGetEndUserLegacyFormat(t *testing.T) {
	c := contractCDP(t, false)
	accounts, err := c.GetEndUser(context.Background(), contractUserID+"-legacy", contractAccess)
	if err != nil {
		t.Fatalf("GetEndUser legacy: %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("accounts len = %d, want 2", len(accounts))
	}
	if accounts[0].Address != "0xlegacy1" || accounts[1].Address != "0xlegacy2" {
		t.Errorf("addresses = %v, want [0xlegacy1 0xlegacy2]", accounts)
	}
}

func TestGetEndUserDecodeError(t *testing.T) {
	c := contractCDP(t, false)
	_, err := c.GetEndUser(context.Background(), contractUserID+"-decode_error", contractAccess)
	if err == nil {
		t.Fatal("GetEndUser decode error: want error, got nil")
	}
	if !strings.Contains(err.Error(), "cdp: end user decode") {
		t.Errorf("error = %q, want contains 'cdp: end user decode'", err.Error())
	}
}

func TestCreateEvmAccountEmptyResponse(t *testing.T) {
	// This test requires a custom server since contractCDP always returns an account
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" || r.Header.Get("X-Wallet-Auth") == "" {
			writeContractJSON(w, http.StatusUnauthorized, map[string]string{"errorType": "unauthorized"})
			return
		}
		writeContractJSON(w, http.StatusOK, map[string][]map[string]string{
			"evmAccountObjects": {},
		})
	}))
	defer srv.Close()

	c := NewClient(contractProjectID)
	c.BaseURL = srv.URL
	vu, _ := time.Parse(time.RFC3339, contractValidUntil)
	// Create a wallet secret to get X-Wallet-Auth
	ws, err := c.CreateWalletSecret(context.Background(), contractUserID, contractAccess, vu, contractSecretID)
	if err != nil {
		t.Fatalf("CreateWalletSecret: %v", err)
	}
	// Override URL for this call
	c.BaseURL = srv.URL
	_, err = c.CreateEvmAccount(context.Background(), contractUserID, contractAccess, ws)
	if err == nil {
		t.Fatal("CreateEvmAccount empty response: want error, got nil")
	}
	if !strings.Contains(err.Error(), "none in response") {
		t.Errorf("error = %q, want contains 'none in response'", err.Error())
	}
}

func TestInitiateMfaEnrollmentIncomplete(t *testing.T) {
	// Test the path where authUrl or secret is empty
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			writeContractJSON(w, http.StatusUnauthorized, map[string]string{"errorType": "unauthorized"})
			return
		}
		// Return incomplete response (missing secret)
		writeContractJSON(w, http.StatusOK, map[string]string{"authUrl": "otpauth://totp/test"})
	}))
	defer srv.Close()

	c := NewClient(contractProjectID)
	c.BaseURL = srv.URL
	_, _, err := c.InitiateMfaEnrollment(context.Background(), contractUserID, contractAccess, "totp")
	if err == nil {
		t.Fatal("InitiateMfaEnrollment incomplete: want error, got nil")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("error = %q, want contains 'incomplete'", err.Error())
	}
}

func TestEmailNoEmailInAuthMethods(t *testing.T) {
	eu := endUser{
		AuthenticationMethods: []struct {
			Type  string `json:"type"`
			Email string `json:"email,omitempty"`
		}{
			{Type: "phone"},
			{Type: "totp"},
		},
	}
	if eu.email() != "" {
		t.Errorf("email() = %q, want empty", eu.email())
	}
}

func TestGetMfaMethodsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			writeContractJSON(w, http.StatusUnauthorized, map[string]string{"errorType": "unauthorized"})
			return
		}
		_, _ = w.Write([]byte(`{invalid mfa json`))
	}))
	defer srv.Close()

	c := NewClient(contractProjectID)
	c.BaseURL = srv.URL
	_, err := c.GetMfaMethods(context.Background(), contractUserID, contractAccess)
	if err == nil {
		t.Fatal("GetMfaMethods decode error: want error, got nil")
	}
	if !strings.Contains(err.Error(), "cdp: mfa status decode") {
		t.Errorf("error = %q, want contains 'cdp: mfa status decode'", err.Error())
	}
}

func TestInitiateEmailOTPDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/auth/init") {
			_, _ = w.Write([]byte(`{invalid init json`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(contractProjectID)
	c.BaseURL = srv.URL
	_, _, err := c.InitiateEmailOTP(context.Background(), contractEmail)
	if err == nil {
		t.Fatal("InitiateEmailOTP decode error: want error, got nil")
	}
	if !strings.Contains(err.Error(), "cdp: init decode") {
		t.Errorf("error = %q, want contains 'cdp: init decode'", err.Error())
	}
}
