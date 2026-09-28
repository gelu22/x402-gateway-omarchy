// Builder-code attribution (ERC-8021 Schema 2, x402 v2 extension).
// Spike 007.x: pure helper, NOT wired into Fetch (wiring = post-GO Etap).
//
// Protocol recap (spec: x402-foundation/x402 specs/extensions/builder_code.md):
//   - the CLIENT attaches service code(s) as `s` in
//     PaymentPayload.extensions["builder-code"];
//   - `a` echoes the server-declared app code, and ONLY when the server
//     declared builder-code in PaymentRequired.extensions (else MUST NOT set);
//   - `w` is facilitator-added at settle; the client MUST NOT set it;
//   - codes match ^[a-z0-9_]{1,32}$; client reservation: max 5 entries.
package x402

import (
	"fmt"
	"regexp"
)

const (
	// BuilderCodeExtensionKey is the PaymentPayload.Extensions key.
	BuilderCodeExtensionKey = "builder-code"
	// MaxClientServiceCodes is the client's `s` reservation (spec).
	MaxClientServiceCodes = 5
)

var builderCodePattern = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

// ValidBuilderCode reports whether code matches the spec pattern.
func ValidBuilderCode(code string) bool { return builderCodePattern.MatchString(code) }

// BuildBuilderExtension returns the value for
// PaymentPayload.extensions["builder-code"].
//
// serviceCodes: our codes (1..=MaxClientServiceCodes, e.g. minted bc_...).
// serverA: app code echoed from PaymentRequired.extensions info.a, or ""
// when the server declared nothing (then `a` is omitted, per spec).
func BuildBuilderExtension(serviceCodes []string, serverA string) (map[string]any, error) {
	if len(serviceCodes) == 0 {
		return nil, fmt.Errorf("x402: builder: no service code")
	}
	if len(serviceCodes) > MaxClientServiceCodes {
		return nil, fmt.Errorf("x402: builder: %d codes exceed client reservation %d",
			len(serviceCodes), MaxClientServiceCodes)
	}
	for _, c := range serviceCodes {
		if !ValidBuilderCode(c) {
			return nil, fmt.Errorf("x402: builder: invalid code %q", c)
		}
	}
	ext := map[string]any{}
	if serverA != "" {
		if !ValidBuilderCode(serverA) {
			return nil, fmt.Errorf("x402: builder: invalid server app code %q", serverA)
		}
		ext["a"] = serverA
	}
	if len(serviceCodes) == 1 {
		ext["s"] = serviceCodes[0]
	} else {
		s := make([]string, len(serviceCodes))
		copy(s, serviceCodes)
		ext["s"] = s
	}
	return ext, nil
}

// ServerAppCode extracts the echoed app code from a decoded
// PaymentRequired.extensions map ("" when the server declared nothing).
func ServerAppCode(extensions map[string]any) string {
	if extensions == nil {
		return ""
	}
	decl, ok := extensions[BuilderCodeExtensionKey].(map[string]any)
	if !ok {
		return ""
	}
	info, ok := decl["info"].(map[string]any)
	if !ok {
		return ""
	}
	a, _ := info["a"].(string)
	if !ValidBuilderCode(a) {
		return ""
	}
	return a
}
