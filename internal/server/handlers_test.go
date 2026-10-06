package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gateway/internal/budget"
	"gateway/internal/cdp"
	"gateway/internal/gateway"
	"gateway/internal/policy"
	"gateway/internal/spend"
)

type testSigner struct{}

func (t *testSigner) Address() string              { return "0xe6D2863Eb960a980eC3714f85568f974d03cE04E" }
func (t *testSigner) UserID() string               { return "test-user" }
func (t *testSigner) AccessToken() (string, error) { return "tok", nil }
func (t *testSigner) WalletSecret() (*cdp.WalletSecret, error) {
	return nil, nil
}

// mockMFAServer defaults to "MFA not enrolled", which makes the sudo gate inert
// (ADR D8) so unrelated tests keep exercising the transport, not the gate.
type mockMFAServer struct {
	verified bool
	enrolled bool
	gateErr  error
}

func (m *mockMFAServer) MfaVerifiedWithin(ctx context.Context, d time.Duration) (bool, bool, string, error) {
	stamp := ""
	if m.verified {
		stamp = "2026-10-01T12:00:00Z"
	}
	return m.verified, m.enrolled, stamp, m.gateErr
}

func (m *mockMFAServer) MfaEnrollInit(ctx context.Context) (otpauthURL, secret, qrDataURI string, err error) {
	return "otpauth://totp/test", "SECRET", "data:image/png;base64,abc", nil
}

func (m *mockMFAServer) MfaEnrollSubmit(ctx context.Context, code string) error {
	return nil
}

func (m *mockMFAServer) MfaVerifyInit(ctx context.Context) error {
	return nil
}

func (m *mockMFAServer) MfaVerifySubmit(ctx context.Context, code string) error {
	return nil
}

func newTestGateway(t *testing.T) *gateway.Gateway {
	t.Helper()
	dir := t.TempDir()
	p := policy.Default()
	if err := p.Save(dir); err != nil {
		t.Fatal(err)
	}
	gw := &gateway.Gateway{
		Spend:        spend.NewTracker(dir),
		Budget:       budget.NewAuthority(dir, nil),
		Signer:       &testSigner{},
		AllowPrivate: true,
		// The /policy POST handler saves next to PolicyPath; without it the
		// test would write policy.json into the source tree.
		PolicyPath: filepath.Join(dir, "policy.json"),
	}
	gw.SetPolicy(p)
	return gw
}

func startTestServer(t *testing.T, gw *gateway.Gateway, mfa MFAAPI) string {
	t.Helper()
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")
	auditPath := filepath.Join(dir, "audit.log")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	go func() {
		_ = Serve(socketPath, "test", gw, &mockPairing{}, mfa, logger, logger, auditPath)
	}()

	deadline := time.After(3 * time.Second)
	for {
		if _, err := net.Dial("unix", socketPath); err == nil {
			return socketPath
		}
		select {
		case <-deadline:
			t.Fatal("socket not ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func testClient(socketPath string) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
}

func TestHandleFetchSuccess(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{"method":"GET","url":"https://example.com"}`))
	resp, err := client.Post("http://localhost/fetch", "application/json", body)
	if err != nil {
		t.Fatalf("fetch request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPaymentRequired {
		// May be 402 (payment required) since no real payment flow
		// The important thing is it didn't return 400 (bad request)
		if resp.StatusCode == http.StatusBadRequest {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("fetch status = %d, want 200 or 402, got 400: %s", resp.StatusCode, raw)
		}
	}
}

func TestHandleFetchBadRequest(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{invalid json`))
	resp, err := client.Post("http://localhost/fetch", "application/json", body)
	if err != nil {
		t.Fatalf("fetch request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("fetch status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleFetchEmptyBody(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte{})
	resp, err := client.Post("http://localhost/fetch", "application/json", body)
	if err != nil {
		t.Fatalf("fetch request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("fetch status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleFetchOverrideSuccess(t *testing.T) {
	gw := newTestGateway(t)
	// 28.1: /fetch-override exists to raise authority, so it now requires an
	// amount (or approve_seller) and always goes through the sudo gate — a
	// fresh, enrolled verification is what a real caller has.
	socketPath := startTestServer(t, gw, &mockMFAServer{verified: true, enrolled: true})
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{"method":"GET","url":"https://example.com","override_amount_micro":5000}`))
	resp, err := client.Post("http://localhost/fetch-override", "application/json", body)
	if err != nil {
		t.Fatalf("fetch-override request: %v", err)
	}
	defer resp.Body.Close()

	// May be 402 or 200 depending on policy, but not 400 (bad request) or 403 (gate).
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusForbidden {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("fetch-override status = %d, want 200 or 402: %s", resp.StatusCode, raw)
	}
}

func TestHandleFetchOverrideBadRequest(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{invalid`))
	resp, err := client.Post("http://localhost/fetch-override", "application/json", body)
	if err != nil {
		t.Fatalf("fetch-override request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("fetch-override status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleMfaEnrollInitNoMFA(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	resp, err := client.Post("http://localhost/mfa/enroll/init", "application/json", nil)
	if err != nil {
		t.Fatalf("mfa enroll init request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("mfa enroll init status = %d, want %d: %s", resp.StatusCode, http.StatusServiceUnavailable, raw)
	}
}

func TestHandleMfaEnrollSubmitInvalidCode(t *testing.T) {
	gw := newTestGateway(t)
	mfa := &mockMFAServer{}
	socketPath := startTestServer(t, gw, mfa)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{"mfa_code":"12345"}`))
	resp, err := client.Post("http://localhost/mfa/enroll/submit", "application/json", body)
	if err != nil {
		t.Fatalf("mfa enroll submit request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("mfa enroll submit status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleMfaEnrollSubmitNonNumeric(t *testing.T) {
	gw := newTestGateway(t)
	mfa := &mockMFAServer{}
	socketPath := startTestServer(t, gw, mfa)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{"mfa_code":"abcdef"}`))
	resp, err := client.Post("http://localhost/mfa/enroll/submit", "application/json", body)
	if err != nil {
		t.Fatalf("mfa enroll submit request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("mfa enroll submit status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleMfaVerifyInitNoMFA(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	resp, err := client.Post("http://localhost/mfa/verify/init", "application/json", nil)
	if err != nil {
		t.Fatalf("mfa verify init request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("mfa verify init status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

func TestHandleMfaVerifySubmitInvalidCode(t *testing.T) {
	gw := newTestGateway(t)
	mfa := &mockMFAServer{}
	socketPath := startTestServer(t, gw, mfa)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{"mfa_code":"12345"}`))
	resp, err := client.Post("http://localhost/mfa/verify/submit", "application/json", body)
	if err != nil {
		t.Fatalf("mfa verify submit request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("mfa verify submit status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleMfaVerifyInitSuccess(t *testing.T) {
	gw := newTestGateway(t)
	mfa := &mockMFAServer{}
	socketPath := startTestServer(t, gw, mfa)
	client := testClient(socketPath)

	resp, err := client.Post("http://localhost/mfa/verify/init", "application/json", nil)
	if err != nil {
		t.Fatalf("mfa verify init request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("mfa verify init status = %d, want %d: %s", resp.StatusCode, http.StatusOK, raw)
	}
}

func TestHandleMfaVerifySubmitSuccess(t *testing.T) {
	gw := newTestGateway(t)
	mfa := &mockMFAServer{}
	socketPath := startTestServer(t, gw, mfa)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{"mfa_code":"123456"}`))
	resp, err := client.Post("http://localhost/mfa/verify/submit", "application/json", body)
	if err != nil {
		t.Fatalf("mfa verify submit request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("mfa verify submit status = %d, want %d: %s", resp.StatusCode, http.StatusOK, raw)
	}
}

func TestHandleMfaEnrollSubmitSuccess(t *testing.T) {
	gw := newTestGateway(t)
	mfa := &mockMFAServer{}
	socketPath := startTestServer(t, gw, mfa)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{"mfa_code":"123456"}`))
	resp, err := client.Post("http://localhost/mfa/enroll/submit", "application/json", body)
	if err != nil {
		t.Fatalf("mfa enroll submit request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("mfa enroll submit status = %d, want %d: %s", resp.StatusCode, http.StatusOK, raw)
	}
}

func TestHandlePolicyPostSuccess(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, &mockMFAServer{verified: true, enrolled: true})
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{"daily_cap_micro_usdc":2000000}`))
	resp, err := client.Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatalf("policy post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		rawBody, _ := io.ReadAll(resp.Body)
		t.Errorf("policy post status = %d, want %d: %s", resp.StatusCode, http.StatusOK, rawBody)
	}
	rawBody, _ := io.ReadAll(resp.Body)
	var result map[string]any
	if err := json.Unmarshal(rawBody, &result); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !result["ok"].(bool) {
		t.Errorf("ok = %v, want true", result["ok"])
	}
}

func TestHandlePairStatus(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	resp, err := client.Get("http://localhost/pair/status")
	if err != nil {
		t.Fatalf("pair status request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("pair status status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandlePairInitBadRequest(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{invalid`))
	resp, err := client.Post("http://localhost/pair/init", "application/json", body)
	if err != nil {
		t.Fatalf("pair init request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("pair init status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandlePairVerifyBadRequest(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{}`))
	resp, err := client.Post("http://localhost/pair/verify", "application/json", body)
	if err != nil {
		t.Fatalf("pair verify request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("pair verify status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandlePairLogout(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	resp, err := client.Post("http://localhost/pair/logout", "application/json", nil)
	if err != nil {
		t.Fatalf("pair logout request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("pair logout status = %d, want %d: %s", resp.StatusCode, http.StatusOK, raw)
	}
}

func TestHandleStatus(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	resp, err := client.Get("http://localhost/status")
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// 34.1: /status must carry the clock difference observed from the CDP Date
// header, so the panel can warn before signatures start failing (T6).
func TestHandleStatusReportsClockSkew(t *testing.T) {
	gw := newTestGateway(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Date", time.Now().Add(-90*time.Second).UTC().Format(http.TimeFormat))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(fake.Close)
	gw.Client = cdp.NewClient("test-project")
	gw.Client.BaseURL = fake.URL
	if _, _, err := gw.Client.PostJSON(context.Background(), "/x", []byte(`{}`), nil); err != nil {
		t.Fatalf("prime clock skew: %v", err)
	}

	socketPath := startTestServer(t, gw, nil)
	resp, err := testClient(socketPath).Get("http://localhost/status")
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var st struct {
		ClockSkewMS int64 `json:"clock_skew_ms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if st.ClockSkewMS < 85000 || st.ClockSkewMS > 95000 {
		t.Fatalf("clock_skew_ms = %d, want ~90000 (server Date 90 s behind)", st.ClockSkewMS)
	}
}

func TestHandlePolicyGet(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	resp, err := client.Get("http://localhost/policy")
	if err != nil {
		t.Fatalf("policy get request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("policy get status = %d, want %d: %s", resp.StatusCode, http.StatusOK, raw)
	}
}

func TestHandlePolicyPostMissingDailyCap(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{"domain_sub_cap_percent":50}`))
	resp, err := client.Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatalf("policy post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("policy post status = %d, want %d: %s", resp.StatusCode, http.StatusBadRequest, raw)
	}
}

func TestHandlePolicyPostInvalidJSON(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	body := bytes.NewReader([]byte(`{invalid`))
	resp, err := client.Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatalf("policy post request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("policy post status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandlePolicyUnsupportedMethod(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	client := testClient(socketPath)

	req, _ := http.NewRequest(http.MethodDelete, "http://localhost/policy", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("policy delete request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("policy delete status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestMapErrorNil(t *testing.T) {
	if got := mapError(nil); got != "ok" {
		t.Errorf("mapError(nil) = %q, want %q", got, "ok")
	}
}

// CONTRACTS §1 names the wire codes; collapsing policy denials to a generic
// "policy_error" hides budget_exceeded/price_changed that agents branch on.
func TestMapErrorWireCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"budget_exceeded", &gateway.PolicyError{Code: "budget_exceeded"}, "budget_exceeded"},
		{"price_changed", &gateway.PolicyError{Code: "price_changed"}, "price_changed"},
		{"unknown_seller", &gateway.PolicyError{Code: "unknown_seller"}, "unknown_seller"},
		{"domain_cap_exceeded", &gateway.PolicyError{Code: "domain_cap_exceeded"}, "domain_cap_exceeded"},
		{"mfa_required", &gateway.PolicyError{Code: "mfa_required"}, "mfa_required"},
		{"invalid_amount", &gateway.PolicyError{Code: "invalid_amount"}, "invalid_amount"},
		{"malformed policy error", &gateway.PolicyError{}, "policy_error"},
		{"paused", gateway.ErrPaused, "paused"},
		{"duplicate_payment", gateway.ErrDuplicate, "duplicate_payment"},
		{"bad_target", gateway.ErrBadTarget, "bad_target"},
		{"signer_error", fmt.Errorf("%w: no key", gateway.ErrSigner), "signer_error"},
		{"no_requirements", gateway.ErrNoRequirements, "no_requirements"},
		{"upstream_error", fmt.Errorf("%w: dial", gateway.ErrUpstream), "upstream_error"},
		{"content_too_large", fmt.Errorf("%w: body", gateway.ErrContentTooLarge), "content_too_large"},
		{"unknown", errors.New("random error"), "server_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapError(tt.err); got != tt.want {
				t.Errorf("mapError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// CONTRACTS §1: policy denials answer 402, other failures 5xx.
func TestMapAndReplyStatusClasses(t *testing.T) {
	srv := &Server{}

	w := httptest.NewRecorder()
	srv.mapAndReply(w, &gateway.PolicyError{Code: "budget_exceeded"})
	if w.Code != http.StatusPaymentRequired {
		t.Errorf("policy denial status = %d, want %d", w.Code, http.StatusPaymentRequired)
	}
	var env map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if env["error"] != "budget_exceeded" {
		t.Errorf("policy denial error = %q, want budget_exceeded", env["error"])
	}

	w = httptest.NewRecorder()
	srv.mapAndReply(w, fmt.Errorf("%w: dial", gateway.ErrUpstream))
	if w.Code != http.StatusBadGateway {
		t.Errorf("upstream failure status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if env["error"] != "upstream_error" {
		t.Errorf("upstream failure error = %q, want upstream_error", env["error"])
	}
}

func TestValidMfaCode(t *testing.T) {
	tests := []struct {
		code  string
		valid bool
	}{
		{"123456", true},
		{"000000", true},
		{"999999", true},
		{"12345", false},
		{"1234567", false},
		{"abcdef", false},
		{"12345a", false},
		{"", false},
		{"1234 6", false},
	}
	for _, tt := range tests {
		got := validMfaCode(tt.code)
		if got != tt.valid {
			t.Errorf("validMfaCode(%q) = %v, want %v", tt.code, got, tt.valid)
		}
	}
}

func TestWriteErrEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	writeErr(w, http.StatusBadRequest, "bad_request", errors.New("test error"))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var envelope map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if envelope["error"] != "bad_request" {
		t.Errorf("error = %q, want %q", envelope["error"], "bad_request")
	}
	if envelope["detail"] != "test error" {
		t.Errorf("detail = %q, want %q", envelope["detail"], "test error")
	}
}

// 24.2 sudo-MFA gate: mutations that raise spending authority need a fresh,
// CDP-attested verification; MFA-off stays inert; anything unconfirmable fails
// closed with an explicit code.
func TestPolicyGateStaleIsRejected(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: true, verified: false})
	body := bytes.NewReader([]byte(`{"daily_cap_micro_usdc":2000000}`))
	resp, err := testClient(socketPath).Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatalf("policy post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "mfa_stale" {
		t.Errorf("error = %q, want mfa_stale", code)
	}
}

func TestPolicyGateUnavailableFailsClosed(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: true, gateErr: errors.New("cdp down")})
	body := bytes.NewReader([]byte(`{"daily_cap_micro_usdc":2000000}`))
	resp, err := testClient(socketPath).Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatalf("policy post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "mfa_unavailable" {
		t.Errorf("error = %q, want mfa_unavailable", code)
	}
}

// Decision A (2026-09-17): without MFA enrollment there is nothing CDP can
// attest, so raising spending authority is refused instead of waved through.
func TestPolicyGateBlocksWithoutEnrollment(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: false})
	body := bytes.NewReader([]byte(`{"daily_cap_micro_usdc":2000000}`))
	resp, err := testClient(socketPath).Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatalf("policy post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "mfa_not_enrolled" {
		t.Errorf("error = %q, want mfa_not_enrolled", code)
	}
}

func TestFetchOverrideGateBlocksWithoutEnrollment(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: false})
	body := bytes.NewReader([]byte(`{"method":"GET","url":"https://example.com","override_amount_micro":5000}`))
	resp, err := testClient(socketPath).Post("http://localhost/fetch-override", "application/json", body)
	if err != nil {
		t.Fatalf("fetch-override: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "mfa_not_enrolled" {
		t.Errorf("error = %q, want mfa_not_enrolled", code)
	}
}

func TestPolicyGateNilMFAFailsClosed(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	body := bytes.NewReader([]byte(`{"daily_cap_micro_usdc":2000000}`))
	resp, err := testClient(socketPath).Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatalf("policy post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "mfa_unavailable" {
		t.Errorf("error = %q, want mfa_unavailable", code)
	}
}

func TestFetchOverrideGateStaleOverrideAmount(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: true, verified: false})
	body := bytes.NewReader([]byte(`{"method":"GET","url":"https://example.com","override_amount_micro":5000}`))
	resp, err := testClient(socketPath).Post("http://localhost/fetch-override", "application/json", body)
	if err != nil {
		t.Fatalf("fetch-override: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "mfa_stale" {
		t.Errorf("error = %q, want mfa_stale", code)
	}
}

func TestFetchOverrideGateStaleApproveSeller(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: true, verified: false})
	body := bytes.NewReader([]byte(`{"method":"GET","url":"https://example.com","approve_seller":true}`))
	resp, err := testClient(socketPath).Post("http://localhost/fetch-override", "application/json", body)
	if err != nil {
		t.Fatalf("fetch-override: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestFetchOverrideGateAllowsFreshVerification(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: true, verified: true})
	body := bytes.NewReader([]byte(`{"method":"GET","url":"https://example.com","override_amount_micro":5000}`))
	resp, err := testClient(socketPath).Post("http://localhost/fetch-override", "application/json", body)
	if err != nil {
		t.Fatalf("fetch-override: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("status = 403, want the request to pass the gate")
	}
}

// 28.1: with neither an amount nor a seller approval, /fetch-override is a
// malformed request and answers 400 before the sudo gate. The nil MFA surface
// proves the order: if the gate ran first we would see 403 mfa_unavailable.
func TestFetchOverrideMalformedBodyIsBadRequestBeforeGate(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	body := bytes.NewReader([]byte(`{"method":"GET","url":"https://example.com"}`))
	resp, err := testClient(socketPath).Post("http://localhost/fetch-override", "application/json", body)
	if err != nil {
		t.Fatalf("fetch-override: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (client error before the gate)", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "bad_request" {
		t.Errorf("error = %q, want bad_request", code)
	}
}

// 28.1 D1: a client-side value error answers 400, not 500/502 — an agent reads
// the code to decide whether to retry.
func TestPolicyPostNegativeCapIsBadRequest(t *testing.T) {
	gw := newTestGateway(t)
	// nil MFA proves the range check runs before the sudo gate (a gated request
	// would answer 403 mfa_unavailable).
	socketPath := startTestServer(t, gw, nil)
	body := bytes.NewReader([]byte(`{"daily_cap_micro_usdc":-1}`))
	resp, err := testClient(socketPath).Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatalf("policy post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "bad_request" {
		t.Errorf("error = %q, want bad_request", code)
	}
}

func TestPolicyPostSubCapOutOfRangeIsBadRequest(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	for _, v := range []string{"-1", "101"} {
		body := bytes.NewReader([]byte(`{"daily_cap_micro_usdc":5000000,"domain_sub_cap_percent":` + v + `}`))
		resp, err := testClient(socketPath).Post("http://localhost/policy", "application/json", body)
		if err != nil {
			t.Fatalf("policy post: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			resp.Body.Close()
			t.Fatalf("domain_sub_cap_percent=%s: status = %d, want 400", v, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// 45.7: amount 0 without approve_seller is malformed (400 before MFA gate).
func TestFetchOverrideAmountZeroWithoutApproveIsBadRequest(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	body := bytes.NewReader([]byte(`{"method":"GET","url":"https://example.com","override_amount_micro":0}`))
	resp, err := testClient(socketPath).Post("http://localhost/fetch-override", "application/json", body)
	if err != nil {
		t.Fatalf("fetch-override: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// 45.7: amount 0 + approve_seller is valid input — reaches sudo gate (not 400).
func TestFetchOverrideAmountZeroWithApproveReachesGate(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil) // nil MFA → 403 mfa_unavailable if gate runs
	body := bytes.NewReader([]byte(`{"method":"GET","url":"https://example.com","override_amount_micro":0,"approve_seller":true}`))
	resp, err := testClient(socketPath).Post("http://localhost/fetch-override", "application/json", body)
	if err != nil {
		t.Fatalf("fetch-override: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (passed validation into MFA gate)", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "mfa_unavailable" {
		t.Errorf("error = %q, want mfa_unavailable", code)
	}
}

// A negative amount is never a valid override, with or without seller approval.
func TestFetchOverrideNegativeAmountIsBadRequest(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	body := bytes.NewReader([]byte(`{"method":"GET","url":"https://example.com","override_amount_micro":-5}`))
	resp, err := testClient(socketPath).Post("http://localhost/fetch-override", "application/json", body)
	if err != nil {
		t.Fatalf("fetch-override: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// errorCodeOf decodes the canonical envelope {"error","detail"}.
func errorCodeOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	var env map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env["error"]
}

// 54.3: bad X-Gateway-Agent → 400 before Gateway.Fetch (paused would not matter).
func TestBadAgentLabelRejectsBeforeFetch(t *testing.T) {
	gw := newTestGateway(t)
	gw.Paused.Store(true) // if Fetch ran, we'd see failed:paused — must not
	sock := startTestServer(t, gw, nil)
	req, err := http.NewRequest(http.MethodPost, "http://localhost/fetch",
		bytes.NewReader([]byte(`{"url":"https://seller.example/x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gateway-Agent", "Bad Label")
	resp, err := testClient(sock).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if code := errorCodeOf(t, resp); code != "bad_agent_label" {
		t.Fatalf("error = %q, want bad_agent_label", code)
	}
}
