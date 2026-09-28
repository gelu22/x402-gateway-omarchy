package chains

import (
	"testing"
)

func TestByCAIP2_Hit(t *testing.T) {
	for _, caip2 := range []string{"eip155:84532", "eip155:8453"} {
		info, ok := ByCAIP2(caip2)
		if !ok {
			t.Fatalf("ByCAIP2(%q) should be ok", caip2)
		}
		if info.CAIP2 != caip2 {
			t.Errorf("ByCAIP2(%q) = %+v, want CAIP2=%q", caip2, info, caip2)
		}
	}
}

func TestByCAIP2_Miss(t *testing.T) {
	info, ok := ByCAIP2("eip155:9999")
	if ok {
		t.Errorf("ByCAIP2(eip155:9999) should be false, got %+v", info)
	}
	if info.CAIP2 != "" {
		t.Errorf("ByCAIP2 miss should return zero ChainInfo, got %+v", info)
	}
}

func TestMustByCAIP2_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustByCAIP2 should panic on unsupported network")
		}
	}()
	MustByCAIP2("eip155:9999")
}

func TestMustByCAIP2_OK(t *testing.T) {
	info := MustByCAIP2("eip155:84532")
	if info.CAIP2 != "eip155:84532" {
		t.Errorf("MustByCAIP2(eip155:84532) = %+v, want CAIP2=eip155:84532", info)
	}
}

func TestUSDCContract(t *testing.T) {
	tests := []struct {
		caip2  string
		expect string
	}{
		{"eip155:84532", "0x036CbD53842c5426634e7929541eC2318f3dCF7e"},
		{"eip155:8453", "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"},
		{"eip155:9999", ""},
	}
	for _, tc := range tests {
		got := USDCContract(tc.caip2)
		if got != tc.expect {
			t.Errorf("USDCContract(%q) = %q, want %q", tc.caip2, got, tc.expect)
		}
	}
}

func TestChainID(t *testing.T) {
	tests := []struct {
		caip2  string
		expect int64
	}{
		{"eip155:84532", 84532},
		{"eip155:8453", 8453},
		{"eip155:9999", 0},
	}
	for _, tc := range tests {
		got := ChainID(tc.caip2)
		if got != tc.expect {
			t.Errorf("ChainID(%q) = %d, want %d", tc.caip2, got, tc.expect)
		}
	}
}

func TestRPCURL(t *testing.T) {
	tests := []struct {
		caip2  string
		expect string
	}{
		{"eip155:84532", "https://sepolia.base.org"},
		{"eip155:8453", "https://mainnet.base.org"},
		{"eip155:9999", ""},
	}
	for _, tc := range tests {
		got := RPCURL(tc.caip2)
		if got != tc.expect {
			t.Errorf("RPCURL(%q) = %q, want %q", tc.caip2, got, tc.expect)
		}
	}
}

func TestName(t *testing.T) {
	tests := []struct {
		caip2  string
		expect string
	}{
		{"eip155:84532", "Base Sepolia"},
		{"eip155:8453", "Base"},
		{"eip155:9999", "eip155:9999"},
	}
	for _, tc := range tests {
		got := Name(tc.caip2)
		if got != tc.expect {
			t.Errorf("Name(%q) = %q, want %q", tc.caip2, got, tc.expect)
		}
	}
}

func TestIsSupported(t *testing.T) {
	tests := []struct {
		caip2  string
		expect bool
	}{
		{"eip155:84532", true},
		{"eip155:8453", true},
		{"eip155:9999", false},
		{"", false},
	}
	for _, tc := range tests {
		got := IsSupported(tc.caip2)
		if got != tc.expect {
			t.Errorf("IsSupported(%q) = %v, want %v", tc.caip2, got, tc.expect)
		}
	}
}

func TestSupportedCAIP2s(t *testing.T) {
	caip2s := SupportedCAIP2s()
	if len(caip2s) != 2 {
		t.Fatalf("SupportedCAIP2s = %v, want 2 entries", caip2s)
	}
	hasSepolia := false
	hasMainnet := false
	for _, c := range caip2s {
		if c == "eip155:84532" {
			hasSepolia = true
		}
		if c == "eip155:8453" {
			hasMainnet = true
		}
	}
	if !hasSepolia {
		t.Error("SupportedCAIP2s missing eip155:84532")
	}
	if !hasMainnet {
		t.Error("SupportedCAIP2s missing eip155:8453")
	}
}
