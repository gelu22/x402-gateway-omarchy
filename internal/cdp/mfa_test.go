package cdp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeMFA emulates the MFA surface recovered from @coinbase/cdp-api-client
// (S7). enrolled controls the mfaMethods on the end-user record.
func fakeMFA(t *testing.T, enrolled bool) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := r.URL.Path
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/mfa/enroll/totp/initiate"):
			writeMFAJSON(w, http.StatusOK, map[string]any{
				"authUrl": "otpauth://totp/Test?secret=JBSWY3DPEHPK3PXP",
				"secret":  "JBSWY3DPEHPK3PXP",
			})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/mfa/enroll/totp/submit"):
			var in struct {
				MfaCode string `json:"mfaCode"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if len(in.MfaCode) != 6 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeMFAJSON(w, http.StatusOK, map[string]any{
				"endUser": map[string]any{"userId": "u-1"},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/mfa/verify/totp/init"):
			writeMFAJSON(w, http.StatusOK, map[string]any{})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/mfa/verify/totp/submit"):
			writeMFAJSON(w, http.StatusOK, map[string]any{})
		case r.Method == http.MethodGet && strings.Contains(p, "/end-users/"):
			methods := map[string]any{}
			if enrolled {
				methods["totp"] = map[string]any{"enrolledAt": "2026-09-01T10:00:00Z"}
			}
			writeMFAJSON(w, http.StatusOK, map[string]any{"mfaMethods": methods})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	c := NewClient("387b5ae3-0000-0000-0000-000000000000")
	c.BaseURL = srv.URL
	return c
}

func writeMFAJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestMfaEnrollInitShape(t *testing.T) {
	c := fakeMFA(t, false)
	authURL, secret, err := c.InitiateMfaEnrollment(context.Background(), "u-1", "tok", "totp")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(authURL, "otpauth://") {
		t.Fatalf("authUrl = %q, want otpauth://", authURL)
	}
	if secret == "" {
		t.Fatal("secret must be non-empty (manual entry fallback)")
	}
	if _, err := QRDataURI(authURL); err != nil {
		t.Fatalf("qr: %v", err)
	} else {
		qr, _ := QRDataURI(authURL)
		if !strings.HasPrefix(qr, "data:image/png;base64,") {
			t.Fatal("qr must be a PNG data URI")
		}
	}
}

func TestMfaEnrollSubmit(t *testing.T) {
	c := fakeMFA(t, false)
	if err := c.SubmitMfaEnrollment(context.Background(), "u-1", "tok", "totp", "123456"); err != nil {
		t.Fatal(err)
	}
}

func TestMfaVerifyInitSubmit(t *testing.T) {
	c := fakeMFA(t, false)
	if err := c.InitiateMfaVerification(context.Background(), "u-1", "tok", "totp"); err != nil {
		t.Fatal(err)
	}
	if err := c.SubmitMfaVerification(context.Background(), "u-1", "tok", "totp", "123456"); err != nil {
		t.Fatal(err)
	}
}

func TestGetMfaMethodsEnrolled(t *testing.T) {
	c := fakeMFA(t, true)
	m, err := c.GetMfaMethods(context.Background(), "u-1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if !m.Enrolled() {
		t.Fatal("want enrolled")
	}
	if got := m.Methods(); len(got) != 1 || got[0] != "totp" {
		t.Fatalf("methods = %v, want [totp]", got)
	}
	if m.Totp == nil || m.Totp.EnrolledAt == "" {
		t.Fatal("totp.enrolledAt must be parsed")
	}
}

func TestGetMfaMethodsEmpty(t *testing.T) {
	c := fakeMFA(t, false)
	m, err := c.GetMfaMethods(context.Background(), "u-1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if m.Enrolled() {
		t.Fatal("want not enrolled")
	}
	if len(m.Methods()) != 0 {
		t.Fatalf("methods = %v, want empty", m.Methods())
	}
}
