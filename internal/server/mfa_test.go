package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gateway/internal/gateway"
)

// fakeMFA implements MFAAPI with scripted results.
type fakeMFA struct {
	otpauth, secret, qr string
	err                 error
	submitCalls         int
	verified, enrolled  bool
}

func (f *fakeMFA) MfaEnrollInit(ctx context.Context) (string, string, string, error) {
	return f.otpauth, f.secret, f.qr, f.err
}

func (f *fakeMFA) MfaEnrollSubmit(ctx context.Context, code string) error {
	f.submitCalls++
	return f.err
}

func (f *fakeMFA) MfaVerifyInit(ctx context.Context) error { return f.err }

func (f *fakeMFA) MfaVerifySubmit(ctx context.Context, code string) error {
	f.submitCalls++
	return f.err
}

// MfaVerifiedWithin defaults to "not enrolled"; the sudo gate refuses that
// state (mfa_not_enrolled), so tests of other paths set verified+enrolled.
func (f *fakeMFA) MfaVerifiedWithin(ctx context.Context, d time.Duration) (bool, bool, error) {
	return f.verified, f.enrolled, f.err
}

func TestMfaEnrollInit(t *testing.T) {
	srv := &Server{Gateway: &gateway.Gateway{}, MFA: &fakeMFA{
		otpauth: "otpauth://totp/x", secret: "ABC", qr: "data:image/png;base64,AAA",
	}}
	req := httptest.NewRequest(http.MethodPost, "/mfa/enroll/init", nil)
	rec := httptest.NewRecorder()
	srv.handleMfaEnrollInit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["otpauth_url"] == "" || out["secret"] == "" || out["qr_data_uri"] == "" {
		t.Fatalf("incomplete enroll payload: %v", out)
	}
}

func TestMfaEnrollSubmitBadCode(t *testing.T) {
	fm := &fakeMFA{}
	srv := &Server{Gateway: &gateway.Gateway{}, MFA: fm}
	for _, body := range []string{`{}`, `{"mfa_code":"12"}`, `{"mfa_code":"abcdef"}`, `not json`} {
		req := httptest.NewRequest(http.MethodPost, "/mfa/enroll/submit", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.handleMfaEnrollSubmit(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want 400", body, rec.Code)
		}
	}
	if fm.submitCalls != 0 {
		t.Fatal("malformed code must not reach MFA backend")
	}
}

func TestMfaEnrollSubmitOK(t *testing.T) {
	fm := &fakeMFA{}
	srv := &Server{Gateway: &gateway.Gateway{}, MFA: fm}
	req := httptest.NewRequest(http.MethodPost, "/mfa/enroll/submit", strings.NewReader(`{"mfa_code":"123456"}`))
	rec := httptest.NewRecorder()
	srv.handleMfaEnrollSubmit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if fm.submitCalls != 1 {
		t.Fatalf("submit calls = %d, want 1", fm.submitCalls)
	}
}

func TestMfaVerifySubmitBadCode(t *testing.T) {
	fm := &fakeMFA{}
	srv := &Server{Gateway: &gateway.Gateway{}, MFA: fm}
	req := httptest.NewRequest(http.MethodPost, "/mfa/verify/submit", strings.NewReader(`{"mfa_code":"12"}`))
	rec := httptest.NewRecorder()
	srv.handleMfaVerifySubmit(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if fm.submitCalls != 0 {
		t.Fatal("malformed code must not reach MFA backend")
	}
}

func TestMfaUnwired503(t *testing.T) {
	srv := &Server{Gateway: &gateway.Gateway{}} // MFA nil
	req := httptest.NewRequest(http.MethodPost, "/mfa/enroll/init", nil)
	rec := httptest.NewRecorder()
	srv.handleMfaEnrollInit(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
