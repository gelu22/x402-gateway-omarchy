package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gateway/internal/budget"
	"gateway/internal/cdp"
	"gateway/internal/gateway"
	"gateway/internal/policy"
	"gateway/internal/spend"
)

// recordingSigner counts signing attempts; policy must reject before any.
type recordingSigner struct {
	signCalls int
}

func (s *recordingSigner) Address() string              { return "0xe6D2863Eb960a980eC3714f85568f974d03cE04E" }
func (s *recordingSigner) UserID() string               { return "test-user" }
func (s *recordingSigner) AccessToken() (string, error) { return "tok", nil }
func (s *recordingSigner) WalletSecret() (*cdp.WalletSecret, error) {
	s.signCalls++
	return nil, nil
}

// mockPairing is a minimal PairingAPI implementation for tests.
type mockPairing struct {
	state     string
	loggedOut bool
}

func (m *mockPairing) InitPairing(ctx context.Context, email string) (flowID, message string, err error) {
	return "flow-1", "Check your email", nil
}

func (m *mockPairing) VerifyPairing(ctx context.Context, flowID, otp string) error {
	m.state = "active"
	return nil
}

func (m *mockPairing) PairState() (state, email, walletAddress string) {
	if m.state == "active" {
		return "active", "user@test.com", "0xe6D2863Eb960a980eC3714f85568f974d03cE04E"
	}
	return "logged_out", "", ""
}

func (m *mockPairing) Logout() error {
	m.state = "logged_out"
	m.loggedOut = true
	return nil
}

const usdcBaseSepolia = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"

func paymentRequiredHeader(amount string) string {
	body := `{"x402Version":2,"accepts":[{"scheme":"exact","network":"eip155:84532","asset":"` +
		usdcBaseSepolia + `","amount":"` + amount +
		`","payTo":"0x19c1d70Df1F5179CfD015A88Acc7371E203B092C","maxTimeoutSeconds":600,"extra":{"name":"USD Coin","version":"2"}}]}`
	return base64.StdEncoding.EncodeToString([]byte(body))
}

func newGateway(t *testing.T, daily, perReq int64) (*gateway.Gateway, *recordingSigner) {
	t.Helper()
	dir := t.TempDir()
	p := policy.Default()
	p.DailyCapMicro = daily
	if err := p.Save(dir); err != nil {
		t.Fatal(err)
	}
	signer := &recordingSigner{}
	gw := &gateway.Gateway{
		Spend:        spend.NewTracker(dir),
		Budget:       budget.NewAuthority(dir, nil),
		Signer:       signer,
		AllowPrivate: true, // httptest binds 127.0.0.1; production leaves false
	}
	gw.SetPolicy(p)
	return gw, signer
}

func TestSocketPerms0600(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "gw.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := os.Chmod(socketPath, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket perms %v, want 0600", perm)
	}
}

func TestFetchOverBudgetDenialBeforeSign(t *testing.T) {
	gw, signer := newGateway(t, 100_000 /* $0.10 */, 10_000 /* $0.01 */)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Payment-Required", paymentRequiredHeader("150000")) // $0.15 > $0.10 daily
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer upstream.Close()

	_, err := gw.Fetch(context.Background(), http.MethodGet, upstream.URL+"/content", nil, nil)
	var perr *gateway.PolicyError
	if err == nil {
		t.Fatal("want policy denial")
	}
	if !errors.As(err, &perr) || perr.Code != "budget_exceeded" {
		t.Fatalf("want budget_exceeded, got %v", err)
	}
	if !perr.CanOverride || perr.AmountMicro != 150_000 {
		t.Fatalf("want overridable 150000, got %+v", perr)
	}
	if signer.signCalls != 0 {
		t.Fatalf("signing attempted %d times before policy check", signer.signCalls)
	}
}

func TestFetchBudgetExceeded(t *testing.T) {
	gw, signer := newGateway(t, 100_000 /* $0.10 daily */, 50_000 /* $0.05 per req */)
	tok, err := gw.Budget.Authorize(90_000, 100_000, 0, "seed") // already spent $0.09 today
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.Budget.Commit(tok); err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Payment-Required", paymentRequiredHeader("20000")) // $0.02 ≤ per-req, but $0.11 > daily
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer upstream.Close()

	_, err = gw.Fetch(context.Background(), http.MethodGet, upstream.URL+"/content", nil, nil)
	var perr *gateway.PolicyError
	if !errors.As(err, &perr) || perr.Code != "budget_exceeded" {
		t.Fatalf("want budget_exceeded, got %v", err)
	}
	if !perr.CanOverride || perr.AmountMicro != 20_000 {
		t.Fatalf("want overridable 20000, got %+v", perr)
	}
	if signer.signCalls != 0 {
		t.Fatal("signing attempted despite budget")
	}
}

func TestFetchPaused(t *testing.T) {
	gw, _ := newGateway(t, 5_000_000, 250_000)
	gw.Paused.Store(true)
	_, err := gw.Fetch(context.Background(), http.MethodGet, "https://example.com/x", nil, nil)
	if err == nil || err.Error() != "paused" {
		t.Fatalf("want paused, got %v", err)
	}
}

func TestFetchBadTarget(t *testing.T) {
	gw, _ := newGateway(t, 5_000_000, 250_000)
	for _, target := range []string{
		"http://localhost/x",
		"http://127.0.0.1/x",
		"http://192.168.1.5/x",
		"file:///etc/passwd",
	} {
		if _, err := gw.Fetch(context.Background(), http.MethodGet, target, nil, nil); err == nil {
			t.Fatalf("target %q must be denied", target)
		}
	}
}

func TestPairLogout(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "gw.sock")
	gw, _ := newGateway(t, 5_000_000, 250_000)

	pairing := &mockPairing{state: "active"}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		_ = Serve(socketPath, "test", gw, pairing, nil, logger, logger, "")
	}()

	// Wait for socket to be ready
	deadline := time.After(3 * time.Second)
	for {
		if _, err := net.Dial("unix", socketPath); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("socket not ready")
		case <-time.After(10 * time.Millisecond):
		}
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}

	// POST /pair/logout
	resp, err := client.Post("http://localhost/pair/logout", "application/json", nil)
	if err != nil {
		t.Fatalf("logout request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("response: %v\n%s", err, body)
	}
	if result["state"] != "logged_out" {
		t.Fatalf("state = %q, want logged_out", result["state"])
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !pairing.loggedOut {
		t.Fatal("pairing.Logout was not called")
	}

	// Idempotent: second logout also succeeds
	resp2, err := client.Post("http://localhost/pair/logout", "application/json", nil)
	if err != nil {
		t.Fatalf("second logout: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second logout status = %d, want 200", resp2.StatusCode)
	}
}

func TestPausePostRoutedAndErrorEnvelopeCompatible(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "gw.sock")
	gw, _ := newGateway(t, 5_000_000, 250_000)

	go func() {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		_ = Serve(socketPath, "test", gw, &mockPairing{}, nil, logger, logger, "")
	}()
	deadline := time.After(3 * time.Second)
	for {
		if _, err := net.Dial("unix", socketPath); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("socket not ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}

	// POST /pause must be routed (panel sends POST).
	pauseBody, _ := json.Marshal(map[string]bool{"paused": true})
	resp, err := client.Post("http://localhost/pause", "application/json", bytes.NewReader(pauseBody))
	if err != nil {
		t.Fatalf("pause request: %v", err)
	}
	pauseRaw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pause status = %d, want 200: %s", resp.StatusCode, pauseRaw)
	}
	var paused map[string]bool
	if err := json.Unmarshal(pauseRaw, &paused); err != nil || !paused["paused"] {
		t.Fatalf("pause response = %s, want {\"paused\":true}", pauseRaw)
	}

	// Error envelope (018.1/018.5): canonical pair error/detail is required;
	// code/message was cut in 018.5 — its presence fails the contract.
	// Fixture shared with plugin/omarchy/test/model.test.mjs ("shared envelope fixture").
	badResp, err := client.Post("http://localhost/pair/verify", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("verify request: %v", err)
	}
	badRaw, _ := io.ReadAll(badResp.Body)
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("verify status = %d, want 400: %s", badResp.StatusCode, badRaw)
	}
	var env map[string]string
	if err := json.Unmarshal(badRaw, &env); err != nil {
		t.Fatalf("envelope decode: %v: %s", err, badRaw)
	}
	for _, k := range []string{"error", "detail"} {
		if env[k] == "" {
			t.Fatalf("envelope missing canonical %q: %s", k, badRaw)
		}
	}
	// Cut fallback (018.5): old pair must be gone.
	for _, k := range []string{"code", "message"} {
		if _, present := env[k]; present {
			t.Fatalf("envelope carries cut fallback %q: %s", k, badRaw)
		}
	}
}
