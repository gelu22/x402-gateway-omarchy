package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gateway/internal/cdp"
)

const (
	testUserID   = "00000000-0000-4000-8000-000000000000"
	testEVMAddr  = "0x0000000000000000000000000000000000000001"
	testUSDCAddr = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"
)

// fakeCDP emulates the CDP End User Accounts REST surface.
type fakeCDP struct {
	srv          *httptest.Server
	refreshCalls atomic.Int64
	twsCalls     atomic.Int64
	failRefresh  atomic.Bool // simulate network failure
	rejectToken  atomic.Bool // simulate revoked refresh token
	limitTWS     atomic.Bool // simulate CDP limit rejection on wallet-secrets
	mux          *http.ServeMux
	// twsKeys records every publicKey registered per walletSecretId, so
	// tests can prove renewal reuses one identity (S6) instead of rotating.
	twsMu   sync.Mutex
	twsKeys map[string][]string
}

func newFakeCDP(t *testing.T) *fakeCDP {
	f := &fakeCDP{mux: http.NewServeMux()}
	f.srv = httptest.NewServer(f.mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCDP) client() *cdp.Client {
	c := cdp.NewClient("387b5ae3-0000-0000-0000-000000000000")
	c.BaseURL = f.srv.URL
	return c
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeCDP) registerRoutes(validUntil time.Time) {
	f.mux.HandleFunc("POST /v2/embedded-wallet-api/projects/{pid}/auth/refresh", func(w http.ResponseWriter, r *http.Request) {
		if f.failRefresh.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if f.rejectToken.Load() {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"errorMessage": "Refresh token not found.", "errorType": "unauthorized"})
			return
		}
		var body struct {
			GrantType    string `json:"grantType"`
			RefreshToken string `json:"refreshToken"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.RefreshToken == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"errorMessage": "Only one refresh token source should be provided"})
			return
		}
		f.refreshCalls.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{

			"endUser":      map[string]any{"userId": testUserID},
			"accessToken":  fmt.Sprintf("access-%d", f.refreshCalls.Load()),
			"validUntil":   validUntil.Format(time.RFC3339),
			"refreshToken": fmt.Sprintf("refresh-%d", f.refreshCalls.Load()),
		})
	})
	f.mux.HandleFunc("/v2/embedded-wallet-api/end-users/{uid}/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/wallet-secrets") && r.Method == http.MethodPut:
			f.twsCalls.Add(1) // every PUT attempt, incl. rejected (proves no retry-storm)
			if f.limitTWS.Load() {
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"errorType": "account_limit_exceeded", "errorMessage": "too many wallet secrets"})
				return
			}
			var body struct {
				WalletSecretID string `json:"walletSecretId"`
				PublicKey      string `json:"publicKey"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.WalletSecretID == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			f.twsMu.Lock()
			if f.twsKeys == nil {
				f.twsKeys = map[string][]string{}
			}
			f.twsKeys[body.WalletSecretID] = append(f.twsKeys[body.WalletSecretID], body.PublicKey)
			f.twsMu.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{
				"walletSecretId": body.WalletSecretID,
				"validUntil":     validUntil.Format(time.RFC3339),
			})
		case strings.HasSuffix(path, "/evm/sign/typed-data"):
			writeJSON(w, http.StatusOK, map[string]string{"signature": "0x" + strings.Repeat("ab", 32)})
		default:
			writeJSON(w, http.StatusOK, map[string]any{
				"userId":            testUserID,
				"evmAccountObjects": []map[string]any{{"address": testEVMAddr}},
			})
		}
	})
}

func fixedNow(offset time.Duration) func() time.Time {
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return base.Add(offset) }
}

// twsIdentities returns the number of distinct registered walletSecretIds and
// whether every publicKey seen for each ID is identical (stable key).
func (f *fakeCDP) twsIdentities() (ids int, uniform bool) {
	f.twsMu.Lock()
	defer f.twsMu.Unlock()
	uniform = true
	for _, keys := range f.twsKeys {
		for i := 1; i < len(keys); i++ {
			if keys[i] != keys[0] {
				uniform = false
			}
		}
	}
	return len(f.twsKeys), uniform
}

// seedStore writes an existing session so New resumes without OTP.
func seedStore(t *testing.T, dir string) {
	st := NewStore(dir)
	err := st.Save(&Data{
		UserID:       testUserID,
		Email:        "user@example.com",
		EVMAddress:   testEVMAddr,
		RefreshToken: "refresh-seed",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResumeAndProactiveRefresh(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, err := New(f.client(), NewStore(dir), slogNop())
	if err != nil {
		t.Fatal(err)
	}
	now := fixedNow(0)
	m.SetNow(now)

	// Fresh resume: access token unknown → first AccessToken triggers refresh.
	tok, err := m.AccessToken()
	if err != nil {
		t.Fatalf("resume refresh: %v", err)
	}
	if tok != "access-1" {
		t.Fatalf("token = %q", tok)
	}
	if f.refreshCalls.Load() != 1 {
		t.Fatalf("refresh calls = %d", f.refreshCalls.Load())
	}

	// Within refreshAhead window → cached token, no extra call.
	if tok2, _ := m.AccessToken(); tok2 != "access-1" || f.refreshCalls.Load() != 1 {
		t.Fatalf("unexpected second refresh")
	}

	// Advance past expiry − refreshAhead → proactive refresh with rotated token.
	m.SetNow(fixedNow(13 * time.Minute))
	if tok3, _ := m.AccessToken(); tok3 != "access-2" {
		t.Fatalf("want access-2 after expiry, got %q", tok3)
	}

	// Persisted store must hold the LATEST refresh token.
	d, err := NewStore(dir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if d.RefreshToken != "refresh-2" {
		t.Fatalf("store refresh token = %q, want refresh-2 (rotation persisted)", d.RefreshToken)
	}
}

func TestLogoutOnRevokedRefresh(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.rejectToken.Store(true)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, err := New(f.client(), NewStore(dir), slogNop())
	if err != nil {
		t.Fatal(err)
	}
	m.SetNow(fixedNow(0))
	if _, err = m.AccessToken(); err == nil {
		t.Fatal("want error on revoked refresh")
	}
	state, _, _ := m.Status()
	if state != StateLoggedOut {
		t.Fatalf("state = %s, want logged_out", state)
	}
	if _, err = NewStore(dir).Load(); err == nil {
		t.Fatal("store must be cleared after logout")
	}
}

func TestFailClosedOnNetworkError(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.failRefresh.Store(true)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	if _, err := m.AccessToken(); err == nil {
		t.Fatal("network failure must surface as error (fail-closed)")
	}
	state, _, _ := m.Status()
	if state != StateActive {
		t.Fatalf("transient network error must NOT log out, state=%s", state)
	}
}

func TestTWSRenewKeepsSingleID(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	ws1, err := m.WalletSecret()
	if err != nil {
		t.Fatal(err)
	}
	ws2, err := m.WalletSecret() // within window → same instance
	if err != nil {
		t.Fatal(err)
	}
	if ws1.ID != ws2.ID {
		t.Fatal("same-window WalletSecret must reuse TWS")
	}

	// Advance beyond twsAhead → RENEW under the SAME ID (S6, supersedes the
	// rotate-with-new-ID behavior of 002.2). twsAhead is 2 min, so with the
	// fixture's validUntil at +15 min the renewal window opens past +13 min.
	m.SetNow(fixedNow(14 * time.Minute))
	ws3, err := m.WalletSecret()
	if err != nil {
		t.Fatal(err)
	}
	if ws3.ID != ws1.ID {
		t.Fatalf("renewed TWS must keep walletSecretId (got %q, want %q)", ws3.ID, ws1.ID)
	}
	if f.twsCalls.Load() < 2 {
		t.Fatalf("tws registrations = %d", f.twsCalls.Load())
	}
	if ids, uniform := f.twsIdentities(); ids != 1 || !uniform {
		t.Fatalf("want exactly 1 distinct TWS id with a stable key, got ids=%d uniform=%v", ids, uniform)
	}
}

// 36.2: renewing must not accumulate key buffers — the superseded TWS is
// wiped in place (mlocked memory included) once the new one is certain.
func TestRenewWipesOldTWS(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	if _, err := m.WalletSecret(); err != nil {
		t.Fatalf("bootstrap TWS: %v", err)
	}
	old := m.tws
	if old == nil || !old.HasKey() {
		t.Fatal("want a live TWS after bootstrap")
	}
	oldID := old.ID

	// Into the renewal window (twsAhead is 2 min, fixture validUntil +15 min).
	m.SetNow(fixedNow(14 * time.Minute))
	ws, err := m.WalletSecret()
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if ws.ID != oldID {
		t.Fatalf("renewed TWS id = %q, want the same %q", ws.ID, oldID)
	}
	if old.HasKey() {
		t.Fatal("superseded TWS still holds its key: renew must Wipe the old buffer")
	}
	if !m.tws.HasKey() {
		t.Fatal("current TWS must stay live after renew")
	}
}

func TestTWSLimitFailsClosedAndNoRetryStorm(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.limitTWS.Store(true)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))

	// First attempt hits CDP and surfaces the typed limit error.
	if _, err := m.WalletSecret(); !errors.Is(err, ErrTWSLimit) {
		t.Fatalf("want ErrTWSLimit, got %v", err)
	}
	if got := f.twsCalls.Load(); got != 1 {
		t.Fatalf("tws registrations = %d, want 1", got)
	}
	// A limit is not an auth rejection: the session must stay active.
	if state, _, _ := m.Status(); state != StateActive {
		t.Fatalf("limit must not log out, state=%s", state)
	}

	// Immediate retry must fail fast WITHOUT another network call (circuit).
	if _, err := m.WalletSecret(); !errors.Is(err, ErrTWSLimit) {
		t.Fatalf("want ErrTWSLimit, got %v", err)
	}
	if got := f.twsCalls.Load(); got != 1 {
		t.Fatalf("circuit broken: tws registrations = %d, want 1", got)
	}

	// After the cooldown the daemon tries again — still limited while CDP says so.
	m.SetNow(fixedNow(6 * time.Minute))
	if _, err := m.WalletSecret(); !errors.Is(err, ErrTWSLimit) {
		t.Fatalf("want ErrTWSLimit, got %v", err)
	}
	if got := f.twsCalls.Load(); got != 2 {
		t.Fatalf("tws registrations = %d, want 2", got)
	}

	// CDP recovers: past the re-armed cooldown the next attempt succeeds
	// and clears the circuit.
	f.limitTWS.Store(false)
	m.SetNow(fixedNow(12 * time.Minute))
	ws, err := m.WalletSecret()
	if err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if ws.ID == "" {
		t.Fatal("recovery must return a TWS")
	}
}

func TestRestartBootstrapsTWS(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m1, _ := New(f.client(), NewStore(dir), slogNop())
	m1.SetNow(fixedNow(0))
	if _, err := m1.WalletSecret(); err != nil {
		t.Fatalf("m1 bootstrap: %v", err)
	}

	// Restart = fresh Manager, same store, no in-memory TWS: must bootstrap
	// cleanly with a new registration (TWS private keys never touch disk).
	m2, _ := New(f.client(), NewStore(dir), slogNop())
	m2.SetNow(fixedNow(0))
	if _, err := m2.WalletSecret(); err != nil {
		t.Fatalf("m2 bootstrap after restart: %v", err)
	}
	if got := f.twsCalls.Load(); got != 2 {
		t.Fatalf("tws registrations = %d, want 2 (one per boot)", got)
	}
}

func TestPairingFlowPersistsIdentity(t *testing.T) {
	dir := t.TempDir()
	f := newFakeCDP(t)
	validUntil := fixedNow(15 * time.Minute)()
	f.mux.HandleFunc("POST /v2/embedded-wallet-api/projects/{pid}/auth/init", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"message": "sent", "flowId": "flow-1"})
	})
	f.mux.HandleFunc("POST /v2/embedded-wallet-api/projects/{pid}/auth/verify/email", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"endUser": map[string]any{
				"userId": testUserID,
				"authenticationMethods": []map[string]any{
					{"type": "email", "email": "user@example.com"},
				},
			},
			"isNewEndUser": true,
			"accessToken":  "access-pair",
			"validUntil":   validUntil.Format(time.RFC3339),
			"refreshToken": "refresh-pair",
		})
	})
	f.registerRoutes(validUntil)

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	flowID, _, err := m.InitPairing(context.Background(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if flowID != "flow-1" {
		t.Fatalf("flowID = %q", flowID)
	}
	if err := m.VerifyPairing(context.Background(), flowID, "123456"); err != nil {
		t.Fatalf("verify pairing: %v", err)
	}
	state, email, addr := m.Status()
	if state != StateActive || email != "user@example.com" || addr != testEVMAddr {
		t.Fatalf("state=%s email=%s addr=%s", state, email, addr)
	}
	d, err := NewStore(dir).Load()
	if err != nil || d.RefreshToken != "refresh-pair" || d.EVMAddress != testEVMAddr {
		t.Fatalf("persisted store: %+v err=%v", d, err)
	}
}

func TestPersistDoesNotOverwriteValidSessionWithEmptyToken(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir) // writes a valid session with refresh-seed

	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, err := New(f.client(), NewStore(dir), slogNop())
	if err != nil {
		t.Fatal(err)
	}
	m.SetNow(fixedNow(0))

	// DON'T call AccessToken() - we want the in-memory state to have
	// the loaded refresh token ("refresh-seed"), not a refreshed one.

	// Simulate the bug: manager's in-memory refreshToken becomes empty
	// (e.g., due to race during shutdown), but store has valid data.
	m.mu.Lock()
	m.refreshToken = ""
	m.mu.Unlock()

	// Call persistLocked (via public Persist) — should NOT overwrite the store
	if err := m.Persist(); err != nil {
		t.Fatalf("Persist() returned error: %v", err)
	}

	// Verify the store still has the original refresh token
	d, err := NewStore(dir).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d.RefreshToken != "refresh-seed" {
		t.Fatalf("store refresh token overwritten to %q, want refresh-seed", d.RefreshToken)
	}
}

func TestPersistSavesCurrentState(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)

	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))

	// Trigger a refresh to get new token
	if _, err := m.AccessToken(); err != nil {
		t.Fatalf("AccessToken: %v", err)
	}

	// Persist should save the rotated token
	if err := m.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	d, err := NewStore(dir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if d.RefreshToken != "refresh-1" {
		t.Fatalf("store refresh token = %q, want refresh-1 (rotated)", d.RefreshToken)
	}
}

func TestLogoutClearsSession(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)

	c := cdp.NewClient("387b5ae3-0000-0000-0000-000000000000")
	m, _ := New(c, NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))

	// Verify session is loaded from seed
	if s, _, _ := m.Status(); s != StateActive {
		t.Fatalf("pre-logout state = %s", s)
	}

	if err := m.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	state, _, _ := m.Status()
	if state != StateLoggedOut {
		t.Fatalf("post-logout state = %s, want %s", state, StateLoggedOut)
	}

	// Verify store file was cleared
	d, err := NewStore(dir).Load()
	if err == nil && d.RefreshToken != "" {
		t.Fatalf("store still has refresh token after logout: %s", d.RefreshToken)
	}

	// Idempotent: second logout succeeds
	if err := m.Logout(); err != nil {
		t.Fatalf("second Logout: %v", err)
	}
	state, _, _ = m.Status()
	if state != StateLoggedOut {
		t.Fatalf("post-logout state = %s", state)
	}
}

func slogNop() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
