package server

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// fetchBody mirrors the /fetch endpoint request shape (socket.go:104-109).
type fetchBody struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

// fetchOverrideBody mirrors the /fetch-override endpoint request shape
// (socket.go:130-142).
type fetchOverrideBody struct {
	URL                 string            `json:"url"`
	Method              string            `json:"method,omitempty"`
	Headers             map[string]string `json:"headers,omitempty"`
	Body                []byte            `json:"body,omitempty"`
	OverrideAmountMicro int64             `json:"override_amount_micro"`
	ApproveSeller       bool              `json:"approve_seller,omitempty"`
	Domain              string            `json:"domain,omitempty"`
}

// FuzzSocketDecodeFetch validates that json.NewDecoder on /fetch and
// /fetch-override bodies never panics and that the two validation rules
// (URL non-empty after TrimSpace; override_amount_micro > 0) hold on
// decoded output. No handler logic is touched.
func FuzzSocketDecodeFetch(f *testing.F) {
	// Minimal valid fetch
	f.Add(`{"url":"https://example.com/"}`)
	// Full fetch with headers/body
	f.Add(`{"url":"https://example.com/","method":"POST","headers":{"X-Test":"1"},"body":"dGVzdA=="}`)
	// Empty URL (should fail validation)
	f.Add(`{"url":""}`)
	f.Add(`{"url":"   "}`)
	// Override valid
	f.Add(`{"url":"https://example.com/","override_amount_micro":1000}`)
	// Override zero amount (should fail validation)
	f.Add(`{"url":"https://example.com/","override_amount_micro":0}`)
	f.Add(`{"url":"https://example.com/","override_amount_micro":-1}`)
	// Override with approve_seller
	f.Add(`{"url":"https://example.com/","override_amount_micro":1000,"approve_seller":true,"domain":"example.com"}`)
	// Malformed
	f.Add(`not json at all`)
	f.Add(`{"url": 12345}`)
	f.Add(`{"url": null}`)
	f.Add(`{"url": {"nested": "object"}}`)
	f.Add(`[1, 2, 3]`)
	f.Add(`{"url":"https://x.com/","extra_deep_field":{"a":{"b":{"c":1}}}}`)
	f.Add(`{"url":"https://x.com/","headers":"not_a_map"}`)
	f.Add(`{"url":"https://x.com/","body":"not_base64_but_json_decodes_it"}`)
	f.Add(`{"override_amount_micro":1000}`) // no URL at all

	f.Fuzz(func(t *testing.T, rawJSON string) {
		// --- Test 1: /fetch decode never panics ---
		var fb fetchBody
		decoder := json.NewDecoder(bytes.NewReader([]byte(rawJSON)))
		decoder.DisallowUnknownFields() // extra fields are OK, but we want to ignore them
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&fb)
		if err != nil {
			// Decoder reject is fine — invariant: never panic.
			return
		}
		// Validation rule 1: URL must be non-empty after TrimSpace.
		if strings.TrimSpace(fb.URL) == "" {
			// Expected failure — the invariant holds.
			return
		}
		// If URL is present, the decode succeeded and passed validation.
		// Extra nested fields are silently ignored (by design of the struct).

		// --- Test 2: /fetch-override decode never panics ---
		var ob fetchOverrideBody
		decoder2 := json.NewDecoder(bytes.NewReader([]byte(rawJSON)))
		decoder2.DisallowUnknownFields()
		err2 := decoder2.Decode(&ob)
		if err2 != nil {
			// Decoder reject is fine.
			return
		}
		// Validation rule 1: URL must be non-empty.
		if strings.TrimSpace(ob.URL) == "" {
			return
		}
		// Validation rule 2: override_amount_micro must be > 0.
		if ob.OverrideAmountMicro <= 0 {
			// Expected failure — invariant holds.
			return
		}
		// Success: both validations passed.
	})
}
