package x402

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func FuzzParsePaymentRequired(f *testing.F) {
	// Valid seed from Node4All test endpoint
	validHeader := `{"x402Version":2,"error":"Payment Required","resource":{"url":"https://sandbox.node4all.com/v1/x402-test","description":"Node4All Fortune","mimeType":"application/json"},"accepts":[{"scheme":"exact","network":"eip155:84532","amount":"2000","asset":"0x036CbD53842c5426634e7929541eC2318f3dCF7e","payTo":"0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6","maxTimeoutSeconds":60,"extra":{"name":"USDC","version":"2"}}]}`
	encoded := base64.StdEncoding.EncodeToString([]byte(validHeader))
	f.Add(encoded)

	// Empty string
	f.Add("")

	// Invalid base64
	f.Add("not base64!")

	// Malformed JSON
	f.Add(base64.StdEncoding.EncodeToString([]byte("{invalid json}")))

	// Missing fields
	f.Add(base64.StdEncoding.EncodeToString([]byte(`{"x402Version":2}`)))

	// Wrong version
	f.Add(base64.StdEncoding.EncodeToString([]byte(`{"x402Version":1,"accepts":[]}`)))

	// Empty accepts
	f.Add(base64.StdEncoding.EncodeToString([]byte(`{"x402Version":2,"accepts":[]}`)))

	// Non-exact scheme
	f.Add(base64.StdEncoding.EncodeToString([]byte(`{"x402Version":2,"accepts":[{"scheme":"upto"}]}`)))

	// Absurd amounts (016.4 money-math hardening)
	f.Add(base64.StdEncoding.EncodeToString([]byte(`{"x402Version":2,"accepts":[{"scheme":"exact","network":"eip155:84532","amount":"9223372036854775807","asset":"0x036CbD53842c5426634e7929541eC2318f3dCF7e","payTo":"0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6","maxTimeoutSeconds":60}]}`)))
	f.Add(base64.StdEncoding.EncodeToString([]byte(`{"x402Version":2,"accepts":[{"scheme":"exact","network":"eip155:84532","amount":"-5","asset":"0x036CbD53842c5426634e7929541eC2318f3dCF7e","payTo":"0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6","maxTimeoutSeconds":60}]}`)))

	// Extreme validity windows (017.2: clamped at selection, parse passes through)
	f.Add(base64.StdEncoding.EncodeToString([]byte(`{"x402Version":2,"accepts":[{"scheme":"exact","network":"eip155:84532","amount":"100","asset":"0x036CbD53842c5426634e7929541eC2318f3dCF7e","payTo":"0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6","maxTimeoutSeconds":999999999999}]}`)))
	f.Add(base64.StdEncoding.EncodeToString([]byte(`{"x402Version":2,"accepts":[{"scheme":"exact","network":"eip155:84532","amount":"100","asset":"0x036CbD53842c5426634e7929541eC2318f3dCF7e","payTo":"0xd275612Bf0BB35638432c4D95eAA8D5d22346Ca6","maxTimeoutSeconds":0}]}`)))

	f.Fuzz(func(t *testing.T, input string) {
		pr, err := ParsePaymentRequired(input)
		if err != nil {
			return // malformed layer rejects; characterized by seeds
		}
		// Success pins the parse contract: valid base64 + valid JSON +
		// at least one accept, amounts passed through verbatim (the parse
		// layer never normalizes money — validation is Check's job).
		if len(pr.Accepts) == 0 {
			t.Fatalf("ParsePaymentRequired(%q) succeeded with empty accepts", input)
		}
		raw, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(input))
		if derr != nil {
			t.Fatalf("ParsePaymentRequired(%q) succeeded on invalid base64", input)
		}
		var m map[string]any
		if jerr := json.Unmarshal(raw, &m); jerr != nil {
			t.Fatalf("ParsePaymentRequired(%q) succeeded on invalid JSON", input)
		}
		rawAcceptsAny, _ := m["accepts"]
		rawAccepts, _ := rawAcceptsAny.([]any)
		if rawAccepts == nil {
			rawAccepts = []any{}
		}
		// Verify that parsed accepts with non-empty amounts match raw amounts.
		// json.Unmarshal silently fills struct fields with zero values when
		// the raw JSON doesn't contain the key — we compare raw→parsed only
		// where the raw key exists.
		for i, ra := range rawAccepts {
			rm, _ := ra.(map[string]any)
			if rm == nil || i >= len(pr.Accepts) {
				continue
			}
			rawAmount, _ := rm["amount"].(string)
			if rawAmount != "" && pr.Accepts[i].Amount != rawAmount {
				t.Fatalf("ParsePaymentRequired(%q): accept %d raw amount %q != parsed %q (silent rewrite)", input, i, rawAmount, pr.Accepts[i].Amount)
			}
		}
	})
}
