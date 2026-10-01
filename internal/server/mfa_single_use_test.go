package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// stampMFA is a fake whose CDP verification stamp the test controls, so
// "same code" and "new code" are distinguishable (46.7).
type stampMFA struct {
	verified bool
	enrolled bool
	stamp    string
}

func (m *stampMFA) MfaVerifiedWithin(context.Context, time.Duration) (bool, bool, string, error) {
	return m.verified, m.enrolled, m.stamp, nil
}
func (m *stampMFA) MfaEnrollInit(context.Context) (string, string, string, error) {
	return "", "", "", nil
}
func (m *stampMFA) MfaEnrollSubmit(context.Context, string) error { return nil }
func (m *stampMFA) MfaVerifyInit(context.Context) error           { return nil }
func (m *stampMFA) MfaVerifySubmit(context.Context, string) error { return nil }

func TestSudoMFAIsSingleUsePerVerification(t *testing.T) {
	m := &stampMFA{verified: true, enrolled: true, stamp: "2026-10-01T12:00:00Z"}
	srv := &Server{MFA: m}

	if !srv.requireSudoMFA(httptest.NewRecorder()) {
		t.Fatal("first raise must pass on a fresh verification")
	}
	rec := httptest.NewRecorder()
	if srv.requireSudoMFA(rec) {
		t.Fatal("second raise on the SAME verification must be refused")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if code := errorCodeOf(t, rec.Result()); code != "mfa_stale" {
		t.Fatalf("error = %q, want mfa_stale", code)
	}
}

func TestSudoMFANewVerificationUnlocksAgain(t *testing.T) {
	m := &stampMFA{verified: true, enrolled: true, stamp: "2026-10-01T12:00:00Z"}
	srv := &Server{MFA: m}

	if !srv.requireSudoMFA(httptest.NewRecorder()) {
		t.Fatal("first raise must pass")
	}
	if srv.requireSudoMFA(httptest.NewRecorder()) {
		t.Fatal("replay on the same code must fail")
	}
	// The owner confirms again: a new CDP verification is a new decision.
	m.stamp = "2026-10-01T12:05:00Z"
	if !srv.requireSudoMFA(httptest.NewRecorder()) {
		t.Fatal("a new verification must unlock one more raise")
	}
}

// TestSudoMFAConcurrentRaisesExactlyOnePasses is the TOCTOU ratchet: check and
// spend happen under one lock, so N concurrent raises on one code cannot all
// pass. Without the lock every one of them would read "not yet consumed".
func TestSudoMFAConcurrentRaisesExactlyOnePasses(t *testing.T) {
	m := &stampMFA{verified: true, enrolled: true, stamp: "2026-10-01T12:00:00Z"}
	srv := &Server{MFA: m}

	const n = 16
	var wg sync.WaitGroup
	results := make([]bool, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = srv.requireSudoMFA(httptest.NewRecorder())
		}(i)
	}
	close(start)
	wg.Wait()

	passed := 0
	for _, ok := range results {
		if ok {
			passed++
		}
	}
	if passed != 1 {
		t.Fatalf("%d of %d concurrent raises passed on one verification, want exactly 1", passed, n)
	}
}

// TestSudoMFANonRaisingRequestsDoNotConsume: read-only traffic must not burn the
// owner's verification, or the panel's own polling would eat the window.
func TestSudoMFANonRaisingRequestsDoNotConsume(t *testing.T) {
	m := &stampMFA{verified: true, enrolled: true, stamp: "2026-10-01T12:00:00Z"}
	srv := &Server{MFA: m, Gateway: newTestGateway(t)}

	// Only requireSudoMFA spends; a plain status read never calls it.
	srv.handleStatus(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/status", nil))
	if !srv.requireSudoMFA(httptest.NewRecorder()) {
		t.Fatal("a raise after a status read must still pass")
	}
}

// TestSudoMFAStaleFailsClosedBeforeConsumption: an old verification never
// reaches the spend step, so the previous stamp is left intact.
func TestSudoMFAStaleFailsClosedBeforeConsumption(t *testing.T) {
	m := &stampMFA{verified: false, enrolled: true, stamp: ""}
	srv := &Server{MFA: m}
	if srv.requireSudoMFA(httptest.NewRecorder()) {
		t.Fatal("stale verification must fail closed")
	}
	m.verified = true
	m.stamp = "2026-10-01T12:00:00Z"
	if !srv.requireSudoMFA(httptest.NewRecorder()) {
		t.Fatal("a stale attempt must not consume the next verification")
	}
}
