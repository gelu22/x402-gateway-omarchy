package x402

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gateway/internal/cdp"
)

func TestBuildTypedDataValid(t *testing.T) {
	req := &PaymentRequirements{
		Extra: map[string]any{
			"name":    "x402 Payment",
			"version": "1",
		},
		Asset: "0x1234567890123456789012345678901234567890",
	}
	auth := &Authorization{
		From:        "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		To:          "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Value:       "1000000000000000000",
		ValidAfter:  "0",
		ValidBefore: "1700000000",
		Nonce:       "0x0000000000000000000000000000000000000000000000000000000000000001",
	}
	td, err := buildTypedData(1, req, auth)
	if err != nil {
		t.Fatalf("buildTypedData: %v", err)
	}
	if td.Domain["name"] != "x402 Payment" {
		t.Errorf("domain name = %v, want 'x402 Payment'", td.Domain["name"])
	}
	if td.Domain["version"] != "1" {
		t.Errorf("domain version = %v, want '1'", td.Domain["version"])
	}
	if td.Domain["chainId"] != int64(1) {
		t.Errorf("domain chainId = %v (%T), want 1", td.Domain["chainId"], td.Domain["chainId"])
	}
	if td.PrimaryType != "TransferWithAuthorization" {
		t.Errorf("primaryType = %q, want %q", td.PrimaryType, "TransferWithAuthorization")
	}
	if td.Message["value"] != "1000000000000000000" {
		t.Errorf("message value = %v, want '1000000000000000000'", td.Message["value"])
	}
}

func TestBuildTypedDataMissingName(t *testing.T) {
	req := &PaymentRequirements{
		Extra: map[string]any{"version": "1"},
	}
	auth := &Authorization{Value: "100"}
	_, err := buildTypedData(1, req, auth)
	if err == nil {
		t.Fatal("buildTypedData missing name: want error, got nil")
	}
	if !strings.Contains(err.Error(), "name/version missing") {
		t.Errorf("error = %q, want contains 'name/version missing'", err.Error())
	}
}

func TestBuildTypedDataMissingVersion(t *testing.T) {
	req := &PaymentRequirements{
		Extra: map[string]any{"name": "x402"},
	}
	auth := &Authorization{Value: "100"}
	_, err := buildTypedData(1, req, auth)
	if err == nil {
		t.Fatal("buildTypedData missing version: want error, got nil")
	}
	if !strings.Contains(err.Error(), "name/version missing") {
		t.Errorf("error = %q, want contains 'name/version missing'", err.Error())
	}
}

func TestBuildTypedDataInvalidAmount(t *testing.T) {
	req := &PaymentRequirements{
		Extra: map[string]any{"name": "x402", "version": "1"},
	}
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"scientific", "1e9", "not decimal"},
		{"hex", "0x1000", "not decimal"},
		{"letters", "abc", "not decimal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth := &Authorization{Value: tt.value}
			_, err := buildTypedData(1, req, auth)
			if err == nil {
				t.Fatalf("buildTypedData(%q): want error, got nil", tt.value)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want contains %q", err.Error(), tt.want)
			}
		})
	}
}

func TestBuildTypedDataInvalidValidityWindows(t *testing.T) {
	req := &PaymentRequirements{
		Extra: map[string]any{"name": "x402", "version": "1"},
	}
	auth := &Authorization{
		Value:       "100",
		ValidAfter:  "not-a-number",
		ValidBefore: "1700000000",
	}
	_, err := buildTypedData(1, req, auth)
	if err == nil {
		t.Fatal("buildTypedData invalid validity: want error, got nil")
	}
	if !strings.Contains(err.Error(), "not decimal") {
		t.Errorf("error = %q, want contains 'not decimal'", err.Error())
	}
}

func TestChecksumOrLower(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0xAbCdEf1234567890AbCdEf1234567890AbCdEf12", "0xabcdef1234567890abcdef1234567890abcdef12"},
		{"0x1234567890123456789012345678901234567890", "0x1234567890123456789012345678901234567890"},
		{"", ""},
		{"not-an-address", "not-an-address"},
		{"0xUPPERCASE1234567890ABCDEF1234567890ABC", "0xuppercase1234567890abcdef1234567890abc"},
	}
	for _, tt := range tests {
		got := checksumOrLower(tt.input)
		if got != tt.want {
			t.Errorf("checksumOrLower(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsHexAddress(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"0x1234567890123456789012345678901234567890", true},
		{"0xAbCdEf1234567890AbCdEf1234567890AbCdEf12", true},
		{"0x123", false},
		{"1234567890123456789012345678901234567890", false},
		{"0x123456789012345678901234567890123456789g", false},
		{"", false},
		{"0xAbCdEf1234567890AbCdEf1234567890AbCdEf1", false},   // 41 chars
		{"0xAbCdEf1234567890AbCdEf1234567890AbCdEf123", false}, // 43 chars
	}
	for _, tt := range tests {
		got := isHexAddress(tt.input)
		if got != tt.want {
			t.Errorf("isHexAddress(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSignAuthorizationViaCDPSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/embedded-wallet-api/end-users/test-user/evm/sign/typed-data" {
			// 0x + 130 hex chars = 132 total
			writeX402JSON(w, http.StatusOK, map[string]string{
				"signature": "0x" + strings.Repeat("a", 130),
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := cdp.NewClient("test-project")
	c.BaseURL = srv.URL

	// Create a valid WalletSecret with a real keypair
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ws := cdp.NewWalletSecret("test-ws-id", time.Time{}, nil, key)
	req := &PaymentRequirements{
		Extra: map[string]any{"name": "x402", "version": "1"},
	}
	auth := &Authorization{
		Value:       "1000000000000000000",
		ValidAfter:  "0",
		ValidBefore: "1700000000",
		Nonce:       "0x0000000000000000000000000000000000000000000000000000000000000001",
	}

	sig, err := SignAuthorizationViaCDP(context.Background(), c, ws, "test-user", "test-token", "0x1234", 1, req, auth)
	if err != nil {
		t.Fatalf("SignAuthorizationViaCDP: %v", err)
	}
	wantSig := "0x" + strings.Repeat("a", 130)
	if sig != wantSig {
		t.Errorf("signature = %q, want %q", sig, wantSig)
	}
}

func TestSignAuthorizationViaCDPInvalidSignatureShape(t *testing.T) {
	tests := []struct {
		name    string
		sig     string
		wantErr string
	}{
		{"too_short", "0x" + strings.Repeat("a", 100), "unexpected signature shape"},
		{"no_prefix", strings.Repeat("a", 130), "unexpected signature shape"},
		{"too_long", "0x" + strings.Repeat("a", 140), "unexpected signature shape"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/embedded-wallet-api/end-users/test-user/evm/sign/typed-data" {
					writeX402JSON(w, http.StatusOK, map[string]string{
						"signature": tt.sig,
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer srv.Close()

			c := cdp.NewClient("test-project")
			c.BaseURL = srv.URL

			key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			ws := cdp.NewWalletSecret("test-ws-id", time.Time{}, nil, key)
			req := &PaymentRequirements{
				Extra: map[string]any{"name": "x402", "version": "1"},
			}
			auth := &Authorization{
				Value:       "1000000000000000000",
				ValidAfter:  "0",
				ValidBefore: "1700000000",
				Nonce:       "0x0000000000000000000000000000000000000000000000000000000000000001",
			}

			_, err := SignAuthorizationViaCDP(context.Background(), c, ws, "test-user", "test-token", "0x1234", 1, req, auth)
			if err == nil {
				t.Fatalf("SignAuthorizationViaCDP(%q): want error, got nil", tt.sig)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want contains %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestSignAuthorizationViaCDPCDPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/embedded-wallet-api/end-users/test-user/evm/sign/typed-data" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal_error"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := cdp.NewClient("test-project")
	c.BaseURL = srv.URL

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ws := cdp.NewWalletSecret("test-ws-id", time.Time{}, nil, key)
	req := &PaymentRequirements{
		Extra: map[string]any{"name": "x402", "version": "1"},
	}
	auth := &Authorization{
		Value:       "1000000000000000000",
		ValidAfter:  "0",
		ValidBefore: "1700000000",
		Nonce:       "0x0000000000000000000000000000000000000000000000000000000000000001",
	}

	_, err := SignAuthorizationViaCDP(context.Background(), c, ws, "test-user", "test-token", "0x1234", 1, req, auth)
	if err == nil {
		t.Fatal("SignAuthorizationViaCDP CDP error: want error, got nil")
	}
}

func TestSignAuthorizationViaCDPDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/embedded-wallet-api/end-users/test-user/evm/sign/typed-data" {
			_, _ = w.Write([]byte(`{invalid json`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := cdp.NewClient("test-project")
	c.BaseURL = srv.URL

	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ws := cdp.NewWalletSecret("test-ws-id", time.Time{}, nil, key)
	req := &PaymentRequirements{
		Extra: map[string]any{"name": "x402", "version": "1"},
	}
	auth := &Authorization{
		Value:       "1000000000000000000",
		ValidAfter:  "0",
		ValidBefore: "1700000000",
		Nonce:       "0x0000000000000000000000000000000000000000000000000000000000000001",
	}

	_, err := SignAuthorizationViaCDP(context.Background(), c, ws, "test-user", "test-token", "0x1234", 1, req, auth)
	if err == nil {
		t.Fatal("SignAuthorizationViaCDP decode error: want error, got nil")
	}
	if !strings.Contains(err.Error(), "x402: sign decode") {
		t.Errorf("error = %q, want contains 'x402: sign decode'", err.Error())
	}
}

func writeX402JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
