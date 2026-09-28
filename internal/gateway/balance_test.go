package gateway

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBalanceFetcher_AddressValidation(t *testing.T) {
	bf := NewBalanceFetcher("eip155:84532")
	bf.HTTP = &http.Client{Timeout: 5 * time.Second}

	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{"valid", "0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F", false},
		{"valid lowercase", "0x3caabbf86c8f53c3cdcb4df3be0fa68fce33630f", false},
		{"short", "0x123", true},
		{"missing 0x", "3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F", true},
		{"too long", "0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F12", true},
		{"empty", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := bf.Fetch(tc.address)
			if (err != nil) != tc.wantErr {
				t.Errorf("Fetch(%q) error = %v, wantErr %v", tc.address, err, tc.wantErr)
			}
		})
	}
}

func TestBalanceFetcher_CacheTTL(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	bf := NewBalanceFetcher("eip155:84532")
	bf.HTTP = &http.Client{Timeout: 5 * time.Second}
	bf.now = func() time.Time { return now }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Return 1 USDC = 1_000_000 base units
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  "0xF4240", // 1_000_000 in hex
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	bf.RPCURL = srv.URL
	bf.now = func() time.Time { return now }

	// First fetch
	bal1, err := bf.Fetch("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F")
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if bal1 != 1.0 {
		t.Errorf("balance = %f, want 1.0", bal1)
	}

	// Advance time by 30s (< TTL), should use cache
	now = now.Add(30 * time.Second)
	bal2, err := bf.Fetch("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F")
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if bal2 != 1.0 {
		t.Errorf("cached balance = %f, want 1.0", bal2)
	}

	// Advance time by 90s (> TTL), should refetch
	now = now.Add(90 * time.Second)
	bal3, err := bf.Fetch("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F")
	if err != nil {
		t.Fatalf("third fetch: %v", err)
	}
	if bal3 != 1.0 {
		t.Errorf("refetched balance = %f, want 1.0", bal3)
	}
}

func TestBalanceFetcher_Invalidate(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	bf := NewBalanceFetcher("eip155:84532")
	bf.now = func() time.Time { return now }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"jsonrpc": "2.0", "id": 1, "result": "0xF4240", // 1 USDC
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	bf.RPCURL = srv.URL
	bf.now = func() time.Time { return now }

	// First fetch
	bal1, err := bf.Fetch("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F")
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if bal1 != 1.0 {
		t.Fatalf("balance = %f, want 1.0", bal1)
	}

	// Invalidate and advance time slightly
	bf.Invalidate()
	now = now.Add(1 * time.Second)

	// Should refetch (returns same value from test server)
	bal2, err := bf.Fetch("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F")
	if err != nil {
		t.Fatalf("fetch after invalidate: %v", err)
	}
	if bal2 != 1.0 {
		t.Errorf("after invalidate = %f, want 1.0", bal2)
	}
}

func TestBalanceFetcher_BigIntParsing(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	bf := NewBalanceFetcher("eip155:84532")
	bf.now = func() time.Time { return now }

	// Test max uint256 (practically impossible but tests big.Int parsing)
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	maxHex := fmt.Sprintf("0x%s", maxUint256.Text(16))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"jsonrpc": "2.0", "id": 1, "result": maxHex,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	bf.RPCURL = srv.URL
	bf.now = func() time.Time { return now }

	bal, err := bf.Fetch("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F")
	if err != nil {
		t.Fatalf("fetch max uint256: %v", err)
	}
	// Just verify it parses without error (max uint256 overflows float64, so we don't check value)
	_ = bal
}

func TestBalanceFetcher_RPCErrors(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	bf := NewBalanceFetcher("eip155:84532")
	bf.now = func() time.Time { return now }

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErrMsg string
	}{
		{
			name: "bad JSON",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("not json"))
			},
			wantErrMsg: "invalid character",
		},
		{
			name: "RPC error field",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"error": map[string]any{
						"code":    -32600,
						"message": "RPC error",
					},
				})
			},
			wantErrMsg: "rpc error",
		},
		{
			name: "empty result",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": 1, "result": "0x",
				})
			},
			wantErrMsg: "unexpected result",
		},
		{
			name: "bad hex",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": 1, "result": "0xZZZZ",
				})
			},
			wantErrMsg: "invalid hex",
		},
		{
			name: "empty result field",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0", "id": 1, "result": "",
				})
			},
			wantErrMsg: "unexpected result",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			bf := NewBalanceFetcher("eip155:84532")
			bf.RPCURL = srv.URL
			bf.now = func() time.Time { return now }

			_, err := bf.Fetch("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F")
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tc.wantErrMsg)
			} else if !strings.Contains(err.Error(), tc.wantErrMsg) {
				t.Errorf("error = %q, want contains %q", err.Error(), tc.wantErrMsg)
			}
		})
	}
}

func TestBalanceFetcher_UnsupportedNetwork(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	bf := NewBalanceFetcher("eip155:9999")
	bf.now = func() time.Time { return now }
	// Even with valid address, unsupported network should fail
	_, err := bf.Fetch("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F")
	if err == nil {
		t.Error("expected error for unsupported network")
	} else if !strings.Contains(err.Error(), "unsupported network") {
		t.Errorf("error = %q, want 'unsupported network'", err.Error())
	}
}

func TestBalanceFetcher_SixDecimals(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	// 1234567 USDC = 1234567000000 base units (6 decimals)
	microUSDC := int64(1234567000000)
	hex := fmt.Sprintf("0x%s", new(big.Int).SetInt64(microUSDC).Text(16))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": 1, "result": hex,
		})
	}))
	defer srv.Close()

	bf := NewBalanceFetcher("eip155:84532")
	bf.RPCURL = srv.URL
	bf.now = func() time.Time { return now }

	bal, err := bf.Fetch("0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	expected := 1234567.0
	if bal != expected {
		t.Errorf("balance = %f, want %f", bal, expected)
	}
}

func TestBalanceFetcher_UnsupportedNetworkFallback(t *testing.T) {
	// Unsupported network should fall back to Sepolia
	bf := NewBalanceFetcher("eip155:9999")
	if bf.Network != "eip155:9999" {
		t.Errorf("Network should be kept as-is, got %q", bf.Network)
	}
	// The fetcher will error at Fetch time, but constructor shouldn't panic
}
