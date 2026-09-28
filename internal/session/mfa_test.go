package session

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// registerMFARoutes adds TOTP MFA routes plus an mfaMethods-carrying exact
// GET user (more specific than the registerRoutes catch-all, so it wins).
// Counters prove caching (no CDP hammering) and fail-fast validation.
func registerMFARoutes(t *testing.T, f *fakeCDP, getCalls, submitCalls, verifyCalls *atomic.Int64) {
	t.Helper()
	f.mux.HandleFunc("POST /v2/embedded-wallet-api/end-users/{uid}/mfa/enroll/totp/initiate", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"authUrl": "otpauth://totp/Test?secret=JBSWY3DPEHPK3PXP",
			"secret":  "JBSWY3DPEHPK3PXP",
		})
	})
	f.mux.HandleFunc("POST /v2/embedded-wallet-api/end-users/{uid}/mfa/enroll/totp/submit", func(w http.ResponseWriter, r *http.Request) {
		submitCalls.Add(1)
		var in struct {
			MfaCode string `json:"mfaCode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if len(in.MfaCode) != 6 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"endUser": map[string]any{"userId": testUserID}})
	})
	f.mux.HandleFunc("POST /v2/embedded-wallet-api/end-users/{uid}/mfa/verify/totp/init", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{})
	})
	f.mux.HandleFunc("POST /v2/embedded-wallet-api/end-users/{uid}/mfa/verify/totp/submit", func(w http.ResponseWriter, _ *http.Request) {
		verifyCalls.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{})
	})
	f.mux.HandleFunc("GET /v2/embedded-wallet-api/end-users/{uid}", func(w http.ResponseWriter, _ *http.Request) {
		getCalls.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"userId":            testUserID,
			"evmAccountObjects": []map[string]any{{"address": testEVMAddr}},
			"mfaMethods":        map[string]any{"totp": map[string]any{"enrolledAt": "2026-09-01T10:00:00Z"}},
		})
	})
}

func TestMfaEnrollInitReturnsQR(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())
	var getCalls, submitCalls, verifyCalls atomic.Int64
	registerMFARoutes(t, f, &getCalls, &submitCalls, &verifyCalls)

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	url, secret, qr, err := m.MfaEnrollInit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "otpauth://") {
		t.Fatalf("otpauth url = %q", url)
	}
	if secret == "" {
		t.Fatal("secret must be non-empty (manual entry fallback)")
	}
	if !strings.HasPrefix(qr, "data:image/png;base64,") {
		t.Fatal("qr must be a PNG data URI (QML displays it, no QR logic there)")
	}
}

func TestMfaEnrollSubmitValidatesCode(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())
	var getCalls, submitCalls, verifyCalls atomic.Int64
	registerMFARoutes(t, f, &getCalls, &submitCalls, &verifyCalls)

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	if err := m.MfaEnrollSubmit(context.Background(), "12"); err == nil {
		t.Fatal("short code must fail")
	}
	if submitCalls.Load() != 0 {
		t.Fatal("malformed code must not reach network")
	}
}

func TestMfaEnrollSubmitSuccessCaches(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())
	var getCalls, submitCalls, verifyCalls atomic.Int64
	registerMFARoutes(t, f, &getCalls, &submitCalls, &verifyCalls)

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	if err := m.MfaEnrollSubmit(context.Background(), "123456"); err != nil {
		t.Fatal(err)
	}
	if submitCalls.Load() != 1 {
		t.Fatalf("submit calls = %d, want 1", submitCalls.Load())
	}
	enrolled, method := m.MfaStatus(context.Background())
	if !enrolled || method != "totp" {
		t.Fatalf("enrolled=%v method=%q after successful submit", enrolled, method)
	}
}

func TestMfaStatusCaches(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())
	var getCalls, submitCalls, verifyCalls atomic.Int64
	registerMFARoutes(t, f, &getCalls, &submitCalls, &verifyCalls)

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	enrolled, method := m.MfaStatus(context.Background())
	if !enrolled || method != "totp" {
		t.Fatalf("enrolled=%v method=%q", enrolled, method)
	}
	if _, _ = m.MfaStatus(context.Background()); getCalls.Load() != 1 {
		t.Fatalf("GET calls = %d, want 1 (cache)", getCalls.Load())
	}
}

func TestMfaVerifySubmitValidatesCode(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())
	var getCalls, submitCalls, verifyCalls atomic.Int64
	registerMFARoutes(t, f, &getCalls, &submitCalls, &verifyCalls)

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	if err := m.MfaVerifySubmit(context.Background(), "12"); err == nil {
		t.Fatal("short code must fail")
	}
	if verifyCalls.Load() != 0 {
		t.Fatal("malformed code must not reach network")
	}
	if err := m.MfaVerifyInit(context.Background()); err != nil {
		t.Fatalf("verify init: %v", err)
	}
}

func TestMfaLoggedOutIsNotEnrolled(t *testing.T) {
	dir := t.TempDir() // empty store → logged out, no network at all
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	if enrolled, method := m.MfaStatus(context.Background()); enrolled || method != "" {
		t.Fatalf("logged out must report no MFA, got %v %q", enrolled, method)
	}
}

// 24.2 sudo gate: freshness must come from CDP, uncached, and anything
// unconfirmable must surface as an error (the transport fails closed on it).
func TestMfaVerifiedWithinFreshness(t *testing.T) {
	tests := []struct {
		name       string
		lastVerify string // "" = omit the field
		enrolled   bool
		wantVerify bool
		wantEnroll bool
		wantErr    bool
	}{
		{"fresh", fixedNow(0)().Add(-1 * time.Minute).Format(time.RFC3339), true, true, true, false},
		{"stale", fixedNow(0)().Add(-5 * time.Minute).Format(time.RFC3339), true, false, true, false},
		{"enrolled but never verified", "", true, false, true, true},
		{"not enrolled", fixedNow(0)().Add(-1 * time.Minute).Format(time.RFC3339), false, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			seedStore(t, dir)
			f := newFakeCDP(t)
			f.registerRoutes(fixedNow(15 * time.Minute)())
			mfaMethods := map[string]any{}
			if tt.enrolled {
				mfaMethods["totp"] = map[string]any{"enrolledAt": "2026-09-01T10:00:00Z"}
			}
			if tt.lastVerify != "" {
				mfaMethods["lastVerificationCompletedAt"] = tt.lastVerify
			}
			f.mux.HandleFunc("GET /v2/embedded-wallet-api/end-users/{uid}", func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusOK, map[string]any{
					"userId":     testUserID,
					"mfaMethods": mfaMethods,
				})
			})

			m, err := New(f.client(), NewStore(dir), slogNop())
			if err != nil {
				t.Fatal(err)
			}
			m.SetNow(fixedNow(0))

			verified, enrolled, err := m.MfaVerifiedWithin(context.Background(), 2*time.Minute)
			if tt.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("MfaVerifiedWithin: %v", err)
			}
			if verified != tt.wantVerify || enrolled != tt.wantEnroll {
				t.Fatalf("verified=%v enrolled=%v, want %v/%v", verified, enrolled, tt.wantVerify, tt.wantEnroll)
			}
		})
	}
}

func TestMfaVerifiedWithinCDPErrorFailsClosed(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())
	f.mux.HandleFunc("GET /v2/embedded-wallet-api/end-users/{uid}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	m, err := New(f.client(), NewStore(dir), slogNop())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.MfaVerifiedWithin(context.Background(), 2*time.Minute); err == nil {
		t.Fatal("want error on CDP failure, got nil")
	}
}

// A dropped session must fail closed: POST /pair/logout is unauthenticated on
// the socket, so "no session = MFA off" would let a local process logout, raise
// the cap without a code, and keep that cap after the owner re-pairs.
func TestMfaVerifiedWithinNoSessionFailsClosed(t *testing.T) {
	m, err := New(nil, NewStore(t.TempDir()), slogNop())
	if err != nil {
		t.Fatal(err)
	}
	verified, enrolled, err := m.MfaVerifiedWithin(context.Background(), 2*time.Minute)
	if err == nil {
		t.Fatalf("logged out: verified=%v enrolled=%v, want error (fail closed)", verified, enrolled)
	}
}

// A timestamp ahead of local time is skew, not freshness: small NTP drift is
// tolerated, a far-future timestamp is rejected.
func TestMfaVerifiedWithinRejectsFutureTimestamps(t *testing.T) {
	tests := []struct {
		name   string
		offset time.Duration
		wantOK bool
	}{
		{"within skew tolerance", 30 * time.Second, true},
		{"far future", 48 * time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			seedStore(t, dir)
			f := newFakeCDP(t)
			f.registerRoutes(fixedNow(15 * time.Minute)())
			at := fixedNow(0)().Add(tt.offset).Format(time.RFC3339)
			f.mux.HandleFunc("GET /v2/embedded-wallet-api/end-users/{uid}", func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusOK, map[string]any{
					"userId": testUserID,
					"mfaMethods": map[string]any{
						"totp":                        map[string]any{"enrolledAt": "2026-09-01T10:00:00Z"},
						"lastVerificationCompletedAt": at,
					},
				})
			})
			m, err := New(f.client(), NewStore(dir), slogNop())
			if err != nil {
				t.Fatal(err)
			}
			m.SetNow(fixedNow(0))
			verified, enrolled, err := m.MfaVerifiedWithin(context.Background(), 2*time.Minute)
			if err != nil {
				t.Fatalf("MfaVerifiedWithin: %v", err)
			}
			if !enrolled {
				t.Fatal("want enrolled")
			}
			if verified != tt.wantOK {
				t.Fatalf("verified = %v, want %v", verified, tt.wantOK)
			}
		})
	}
}

// Logout (not just "never paired") must fail closed within the same process,
// even though the session store is cleared.
func TestMfaVerifiedWithinAfterLogoutFailsClosed(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())
	m, err := New(f.client(), NewStore(dir), slogNop())
	if err != nil {
		t.Fatal(err)
	}
	m.SetNow(fixedNow(0))
	if err := m.Logout(); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, _, err := m.MfaVerifiedWithin(context.Background(), 2*time.Minute); err == nil {
		t.Fatal("after logout: want error (fail closed), got nil")
	}
}

// 34.3: MfaStatus keeps the last known enrollment when CDP is unavailable
// ("fail-safe display, never flaps to false", mfa.go:87-89). The fallback
// (cachedMFA) had 0% coverage, so its two call sites were untested.

func TestMfaStatusFallsBackToCachedOnCDPError(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())
	var getCalls, submitCalls, verifyCalls atomic.Int64
	registerMFARoutes(t, f, &getCalls, &submitCalls, &verifyCalls)

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	// A previous read established the state; the cache is stale, so the next
	// call must try CDP.
	stale := fixedNow(0)().Add(-2 * mfaCacheTTL)
	m.mfaEnrolled = true
	m.mfaMethod = "totp"
	m.mfaCheckedAt = stale

	f.srv.Close() // CDP unreachable → GetMfaMethods fails

	enrolled, method := m.MfaStatus(context.Background())
	if !enrolled || method != "totp" {
		t.Fatalf("fallback = (%v, %q), want (true, totp)", enrolled, method)
	}
	if !m.mfaCheckedAt.Equal(stale) {
		t.Fatal("fallback must not refresh mfaCheckedAt (the next call must retry CDP)")
	}
}

func TestMfaStatusFallsBackToCachedWhenTokenUnavailable(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.failRefresh.Store(true) // no access token can be obtained
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	m.mfaEnrolled = true
	m.mfaMethod = "totp"
	m.mfaCheckedAt = fixedNow(0)().Add(-2 * mfaCacheTTL)

	enrolled, method := m.MfaStatus(context.Background())
	if !enrolled || method != "totp" {
		t.Fatalf("fallback = (%v, %q), want (true, totp)", enrolled, method)
	}
}

func TestMfaStatusFallbackWithoutPriorReadStaysEmpty(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.failRefresh.Store(true)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	m.mfaCheckedAt = fixedNow(0)().Add(-2 * mfaCacheTTL) // stale, nothing cached

	enrolled, method := m.MfaStatus(context.Background())
	if enrolled || method != "" {
		t.Fatalf("fallback invented state: (%v, %q), want (false, \"\")", enrolled, method)
	}
}
