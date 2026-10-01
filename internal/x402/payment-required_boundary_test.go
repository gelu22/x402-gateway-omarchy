package x402

import (
	"encoding/base64"
	"testing"
)

// Boundary ratchets for the 402 parser (47.5). The parser is permissive BY
// CONTRACT — x402 v2 is extension-based, so unknown fields from a seller are
// forward-compatibility, not an attack surface. The ceiling is CheckStatic on
// the content after parsing. These tests pin BOTH sides, so that "hardening"
// the parser (DisallowUnknownFields) breaks a pin consciously instead of
// silently rejecting legitimate sellers.

const boundaryAccepts = `"accepts":[{"scheme":"exact","network":"eip155:84532","amount":"100","asset":"0x036CbD53842c5426634e7929541eC2318f3dCF7e","payTo":"0x19c1d70Df1F5179CfD015A88Acc7371E203B092C","maxTimeoutSeconds":60}]`

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// Unknown top-level fields must be ignored: a seller sending a newer protocol
// feature still parses, and the known content still selects.
func TestBoundaryUnknownTopLevelFieldIgnored(t *testing.T) {
	pr, err := ParsePaymentRequired(b64(`{"x402Version":2,"futureFeature":{"a":1},` + boundaryAccepts + `}`))
	if err != nil {
		t.Fatalf("unknown top-level field must not reject the seller: %v", err)
	}
	if _, err := pr.SelectRequirements(); err != nil {
		t.Fatalf("SelectRequirements must still work: %v", err)
	}
}

// Unknown fields INSIDE an accepts entry must be ignored too — that is where
// per-scheme extensions live.
func TestBoundaryUnknownAcceptsFieldIgnored(t *testing.T) {
	accepts := `"accepts":[{"scheme":"exact","network":"eip155:84532","amount":"100","asset":"0x036CbD53842c5426634e7929541eC2318f3dCF7e","payTo":"0x19c1d70Df1F5179CfD015A88Acc7371E203B092C","maxTimeoutSeconds":60,"sellerExtension":"x"}]`
	pr, err := ParsePaymentRequired(b64(`{"x402Version":2,` + accepts + `}`))
	if err != nil {
		t.Fatalf("unknown accepts field must not reject the seller: %v", err)
	}
	if _, err := pr.SelectRequirements(); err != nil {
		t.Fatalf("SelectRequirements must still work: %v", err)
	}
}

// The strict side: permissive on unknown fields does NOT mean permissive on
// content. Version and accepts are enforced exactly as before.
func TestBoundaryVersionStillEnforced(t *testing.T) {
	if _, err := ParsePaymentRequired(b64(`{"x402Version":9,` + boundaryAccepts + `}`)); err == nil {
		t.Fatal("wrong x402Version must be rejected")
	}
}

func TestBoundaryEmptyAcceptsStillEnforced(t *testing.T) {
	if _, err := ParsePaymentRequired(b64(`{"x402Version":2,"accepts":[],"futureField":1}`)); err == nil {
		t.Fatal("empty accepts must be rejected even alongside unknown fields")
	}
}
