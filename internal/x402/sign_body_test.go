package x402

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestSignBodyTypedDataIsRawObject (47.4) pins the wire shape of the CDP sign
// request: body["typedData"] must be a JSON OBJECT semantically equal to the
// marshalled typed data — not a string, not a double-encoded blob. The old
// round-trip (marshal → unmarshal into any → marshal) hid the type; RawMessage
// makes the failure mode a compile-visible choice again.
//
// The comparison is SEMANTIC (deep-equal after unmarshal), not byte-equal:
// RawMessage preserves struct field order from the first marshal, while the old
// round-trip sorted keys through the map — bytes may differ, CDP parses JSON.
func TestSignBodyTypedDataIsRawObject(t *testing.T) {
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
	td, err := buildTypedData(84532, req, auth)
	if err != nil {
		t.Fatalf("buildTypedData: %v", err)
	}
	tdJSON, err := json.Marshal(td)
	if err != nil {
		t.Fatalf("marshal typed data: %v", err)
	}

	// The production embedding: RawMessage in the body map.
	body := map[string]any{
		"address":        "0x0000000000000000000000000000000000000001",
		"typedData":      json.RawMessage(tdJSON),
		"walletSecretId": "test-secret",
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}

	// typedData must decode as a JSON OBJECT, not a string.
	var asString string
	if err := json.Unmarshal(decoded["typedData"], &asString); err == nil {
		t.Fatalf("typedData was embedded as a STRING (%q) — CDP would reject it", asString)
	}
	var asObj map[string]any
	if err := json.Unmarshal(decoded["typedData"], &asObj); err != nil {
		t.Fatalf("typedData is not a JSON object: %v", err)
	}

	// Semantic equality with the typed data itself.
	var want map[string]any
	if err := json.Unmarshal(tdJSON, &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if !reflect.DeepEqual(asObj, want) {
		t.Fatalf("typedData drifted from the marshalled typed data:\n got: %v\nwant: %v", asObj, want)
	}
}

// TestSignBodyTypedDataStringWouldFail documents the failure mode the ratchet
// guards against: embedding the JSON as a Go string produces a JSON string in
// the body, which CDP would reject. Written as a negative proof so the next
// reader sees WHY RawMessage and not string(tdJSON).
func TestSignBodyTypedDataStringWouldFail(t *testing.T) {
	tdJSON := []byte(`{"domain":{"name":"x"},"primaryType":"Transfer"}`)
	body := map[string]any{
		"typedData": string(tdJSON), // the wrong embedding
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	var asString string
	if err := json.Unmarshal(decoded["typedData"], &asString); err != nil {
		t.Fatalf("precondition: expected a string embedding, got %v", err)
	}
	if asString != string(tdJSON) {
		t.Fatalf("string round-trip drifted: %q", asString)
	}
}
