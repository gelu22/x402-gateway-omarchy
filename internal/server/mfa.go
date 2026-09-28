package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// MFAAPI is the MFA surface implemented by session.Manager.
type MFAAPI interface {
	MfaEnrollInit(ctx context.Context) (otpauthURL, secret, qrDataURI string, err error)
	MfaEnrollSubmit(ctx context.Context, code string) error
	MfaVerifyInit(ctx context.Context) error
	MfaVerifySubmit(ctx context.Context, code string) error
	// MfaVerifiedWithin reports whether a CDP MFA verification completed within
	// d; enrolled=false (MFA off, ADR D8) or no session means the sudo gate is
	// inert, and an error means the caller must fail closed.
	MfaVerifiedWithin(ctx context.Context, d time.Duration) (verified bool, enrolled bool, err error)
}

// mfaSudoWindow bounds how old a CDP verification may be for a mutation that
// raises spending authority (cap change, seller approval, over-budget override).
// Short by design: the point is a human deciding now, not a session-long pass.
const mfaSudoWindow = 120 * time.Second

// requireSudoMFA enforces the sudo gate. It returns true when the request may
// proceed; otherwise it writes the CONTRACTS §1 envelope and returns false.
//
// Fail-closed, and MFA is a precondition rather than an option (ADR D8, decision
// A of 2026-09-17): with no enrollment there is nothing CDP can attest, so
// raising spending authority is refused with mfa_not_enrolled instead of being
// waved through. The distinct codes exist because the UI must tell "enroll MFA"
// apart from "confirm with a code".
func (s *Server) requireSudoMFA(w http.ResponseWriter) bool {
	if s.MFA == nil {
		writeErr(w, http.StatusForbidden, "mfa_unavailable", errMFANotWired)
		return false
	}
	verified, enrolled, err := s.MFA.MfaVerifiedWithin(context.Background(), mfaSudoWindow)
	if err != nil {
		writeErr(w, http.StatusForbidden, "mfa_unavailable", err)
		return false
	}
	if !enrolled {
		writeErr(w, http.StatusForbidden, "mfa_not_enrolled",
			errors.New("MFA is not enrolled: enroll TOTP before raising spending authority"))
		return false
	}
	if !verified {
		writeErr(w, http.StatusForbidden, "mfa_stale", errors.New("mfa verification is older than allowed"))
		return false
	}
	return true
}

var errMFANotWired = errors.New("mfa not wired")

// validMfaCode mirrors the daemon rule (CDP ^\d{6}$); transport-level
// validation so malformed codes fail fast with 400, not 502.
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

func (s *Server) handleMfaEnrollInit(w http.ResponseWriter, _ *http.Request) {
	if s.MFA == nil {
		writeErr(w, http.StatusServiceUnavailable, "mfa_unavailable", errMFANotWired)
		return
	}
	ctx := context.Background()
	otpauthURL, secret, qr, err := s.MFA.MfaEnrollInit(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "mfa_error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"otpauth_url": otpauthURL,
		"secret":      secret,
		"qr_data_uri": qr,
	})
}

func (s *Server) handleMfaEnrollSubmit(w http.ResponseWriter, r *http.Request) {
	s.handleMfaCode(w, r, func(code string) error {
		if s.MFA == nil {
			return errMFANotWired
		}
		ctx := context.Background()
		return s.MFA.MfaEnrollSubmit(ctx, code)
	})
}

func (s *Server) handleMfaVerifyInit(w http.ResponseWriter, _ *http.Request) {
	if s.MFA == nil {
		writeErr(w, http.StatusServiceUnavailable, "mfa_unavailable", errMFANotWired)
		return
	}
	ctx := context.Background()
	if err := s.MFA.MfaVerifyInit(ctx); err != nil {
		writeErr(w, http.StatusBadGateway, "mfa_error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMfaVerifySubmit(w http.ResponseWriter, r *http.Request) {
	s.handleMfaCode(w, r, func(code string) error {
		if s.MFA == nil {
			return errMFANotWired
		}
		ctx := context.Background()
		if err := s.MFA.MfaVerifySubmit(ctx, code); err != nil {
			return err
		}
		// A verified code completes the payment that was blocked on it (30.2b):
		// every pending fetch retries once in the background.
		s.mfaWait().notifyVerified()
		return nil
	})
}

// handleMfaCode decodes {mfa_code}, validates the shape (400) and runs fn;
// daemon errors surface as 502 (mfa_error), unwired as 503.
func (s *Server) handleMfaCode(w http.ResponseWriter, r *http.Request, fn func(code string) error) {
	var body struct {
		MfaCode string `json:"mfa_code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !validMfaCode(body.MfaCode) {
		writeErr(w, http.StatusBadRequest, "bad_request", fmt.Errorf("mfa_code must be 6 digits"))
		return
	}
	if err := fn(body.MfaCode); err != nil {
		if errors.Is(err, errMFANotWired) {
			writeErr(w, http.StatusServiceUnavailable, "mfa_unavailable", err)
			return
		}
		writeErr(w, http.StatusBadGateway, "mfa_error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
