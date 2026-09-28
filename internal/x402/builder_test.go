package x402

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// Spike 007.3: proves the exact emission shape (unit level, no network).
// 007.5 wires BuildBuilderExtension into doFetch (gateway.go);
// TestEnvelopeCarriesBuilderExtension mirrors that wiring end to end
// (minus signing) and asserts the wire format a mock seller would see.

func TestBuilderExtensionSingleCode(t *testing.T) {
	ext, err := BuildBuilderExtension([]string{"bc_x402_gateway"}, "")
	if err != nil {
		t.Fatalf("single code: %v", err)
	}
	if ext["s"] != "bc_x402_gateway" {
		t.Fatalf("want scalar s, got %v", ext["s"])
	}
	if _, ok := ext["a"]; ok {
		t.Fatal("a must be omitted when server declared nothing")
	}
	if _, ok := ext["w"]; ok {
		t.Fatal("w must never be set by the client")
	}
}

func TestBuilderExtensionEchoesServerA(t *testing.T) {
	ext, err := BuildBuilderExtension([]string{"bc_x402_gateway"}, "bc_creative_tim")
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if ext["a"] != "bc_creative_tim" {
		t.Fatalf("want echoed a, got %v", ext["a"])
	}
}

func TestBuilderExtensionValidation(t *testing.T) {
	if _, err := BuildBuilderExtension(nil, ""); err == nil {
		t.Fatal("empty codes must fail")
	}
	if _, err := BuildBuilderExtension([]string{"UPPERCASE"}, ""); err == nil {
		t.Fatal("uppercase must fail pattern")
	}
	many := []string{"a1", "a2", "a3", "a4", "a5", "a6"}
	if _, err := BuildBuilderExtension(many, ""); err == nil {
		t.Fatal("6 codes exceed client reservation 5")
	}
	if _, err := BuildBuilderExtension([]string{"bc_ok"}, "BAD CODE!"); err == nil {
		t.Fatal("invalid server a must fail")
	}
}

func TestServerAppCodeExtraction(t *testing.T) {
	raw := `{"builder-code":{"info":{"a":"bc_creative_tim"},"schema":{}}}`
	var extensions map[string]any
	if err := json.Unmarshal([]byte(raw), &extensions); err != nil {
		t.Fatal(err)
	}
	if got := ServerAppCode(extensions); got != "bc_creative_tim" {
		t.Fatalf("want bc_creative_tim, got %q", got)
	}
	if got := ServerAppCode(nil); got != "" {
		t.Fatalf("nil extensions want empty, got %q", got)
	}
	if got := ServerAppCode(map[string]any{}); got != "" {
		t.Fatalf("no declaration want empty, got %q", got)
	}
}

// TestEnvelopeCarriesBuilderExtension mirrors the gateway wiring
// (BuildBuilderExtension → EncodePaymentSignatureHeader) and asserts the
// exact wire shape a mock seller would decode from PAYMENT-SIGNATURE.
func TestEnvelopeCarriesBuilderExtension(t *testing.T) {
	auth := &Authorization{From: "0xe6D2863Eb960a980eC3714f85568f974d03cE04E", To: "0x19c1d70Df1F5179CfD015A88Acc7371E203B092C", Value: "100", ValidAfter: "0", ValidBefore: "9999999999", Nonce: "0x00"}
	req := &PaymentRequirements{Scheme: "exact", Network: "eip155:84532", Asset: "0x036CbD53842c5426634e7929541eC2318f3dCF7e", Amount: "100", PayTo: "0x19c1d70Df1F5179CfD015A88Acc7371E203B092C", MaxTimeoutSeconds: 300}

	// Seller silent: only our s.
	ext, err := BuildBuilderExtension([]string{"bc_lgtoqsts"}, ServerAppCode(nil))
	if err != nil {
		t.Fatal(err)
	}
	header, err := EncodePaymentSignatureHeader(nil, req, auth, "0xdeadbeef", map[string]any{BuilderCodeExtensionKey: ext})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(header)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	got, ok := wire["extensions"].(map[string]any)[BuilderCodeExtensionKey].(map[string]any)
	if !ok {
		t.Fatalf("missing extensions.builder-code in %s", raw)
	}
	if got["s"] != "bc_lgtoqsts" {
		t.Fatalf("want s=bc_lgtoqsts, got %v", got["s"])
	}
	if _, ok := got["a"]; ok {
		t.Fatalf("a must be absent when seller silent, got %v", got)
	}

	// Seller declares: echo a + our s.
	ext2, err := BuildBuilderExtension([]string{"bc_lgtoqsts"}, "bc_creative_tim")
	if err != nil {
		t.Fatal(err)
	}
	header2, err := EncodePaymentSignatureHeader(nil, req, auth, "0xdeadbeef", map[string]any{BuilderCodeExtensionKey: ext2})
	if err != nil {
		t.Fatal(err)
	}
	raw2, _ := base64.StdEncoding.DecodeString(header2)
	var wire2 map[string]any
	if err := json.Unmarshal(raw2, &wire2); err != nil {
		t.Fatal(err)
	}
	got2 := wire2["extensions"].(map[string]any)[BuilderCodeExtensionKey].(map[string]any)
	if got2["a"] != "bc_creative_tim" || got2["s"] != "bc_lgtoqsts" {
		t.Fatalf("want echoed a + s, got %v", got2)
	}

	// Nil extensions: wire identical to no-extension (backward compat).
	plain, err := EncodePaymentSignatureHeader(nil, req, auth, "0xdeadbeef", nil)
	if err != nil {
		t.Fatal(err)
	}
	rawPlain, _ := base64.StdEncoding.DecodeString(plain)
	var wirePlain map[string]any
	if err := json.Unmarshal(rawPlain, &wirePlain); err != nil {
		t.Fatal(err)
	}
	if _, ok := wirePlain["extensions"]; ok {
		t.Fatalf("nil extensions must omit the key, got %s", rawPlain)
	}
}
