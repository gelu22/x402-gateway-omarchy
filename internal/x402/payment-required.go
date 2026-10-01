// Package x402 implements the minimal buyer side of the x402 v2 protocol
// needed by the S3 spike: parsing PAYMENT-REQUIRED, building the EIP-3009
// authorization, signing it via CDP and encoding the PAYMENT-SIGNATURE header.
package x402

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"gateway/internal/chains"
)

// MaxAuthorizationTTLSeconds caps the seller-advertised maxTimeoutSeconds.
// It bounds the EIP-3009 validBefore window (CDP ecosystem default: 300).
// Clamped, never rejected — a slow seller still settles within the default.
const MaxAuthorizationTTLSeconds = 300

// PaymentRequirements mirrors @x402/core types (v2).
type PaymentRequirements struct {
	Scheme            string         `json:"scheme"`
	Network           string         `json:"network"`
	Asset             string         `json:"asset"`
	Amount            string         `json:"amount"`
	PayTo             string         `json:"payTo"`
	MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
	Extra             map[string]any `json:"extra"`
}

// PaymentRequired is the decoded PAYMENT-REQUIRED header payload.
type PaymentRequired struct {
	X402Version int                   `json:"x402Version"`
	Error       string                `json:"error,omitempty"`
	Resource    map[string]any        `json:"resource"`
	Accepts     []PaymentRequirements `json:"accepts"`
	Extensions  map[string]any        `json:"extensions,omitempty"`
}

// ParsePaymentRequired decodes the base64 PAYMENT-REQUIRED header value.
//
// Deliberately NO DisallowUnknownFields (47.5): x402 v2 is extension-based by
// contract (PaymentRequired.Extensions carries forward-compatible fields), so a
// strict parser would reject legitimate sellers before their content is even
// looked at. The security ceiling is not the parser's shape check but
// policy.CheckStatic on the CONTENT after parsing — canonical amount, pinned
// asset, network whitelist. Unknown fields are ignored; the version and the
// accepts list are still enforced. Pinned by payment-required_boundary_test.go.
func ParsePaymentRequired(header string) (*PaymentRequired, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header))
	if err != nil {
		return nil, fmt.Errorf("x402: header base64: %w", err)
	}
	var pr PaymentRequired
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, fmt.Errorf("x402: header json: %w (%s)", err, snippet(raw))
	}
	if pr.X402Version != x402Version {
		return nil, fmt.Errorf("x402: unsupported version %d (want %d)", pr.X402Version, x402Version)
	}
	if len(pr.Accepts) == 0 {
		return nil, fmt.Errorf("x402: no accepts in payment required (%s)", snippet(raw))
	}
	return &pr, nil
}

func (pr *PaymentRequired) SelectRequirements() (*PaymentRequirements, error) {
	for i := range pr.Accepts {
		r := &pr.Accepts[i]
		if r.Scheme != "exact" {
			continue
		}
		pinned := chains.USDCContract(r.Network)
		if pinned == "" {
			continue
		}
		if !strings.EqualFold(r.Asset, pinned) {
			return nil, fmt.Errorf("x402: policy: asset %s is not pinned USDC for %s", r.Asset, r.Network)
		}
		if r.PayTo == "" || r.Amount == "" || r.MaxTimeoutSeconds <= 0 {
			return nil, fmt.Errorf("x402: incomplete requirements: payTo/amount/maxTimeout missing")
		}
		// Clamp the seller-controlled validity window (never trust it unbounded).
		if r.MaxTimeoutSeconds > MaxAuthorizationTTLSeconds {
			r.MaxTimeoutSeconds = MaxAuthorizationTTLSeconds
		}
		return r, nil
	}
	return nil, fmt.Errorf("x402: policy: no acceptable requirement (want scheme=exact, network eip155:8453|84532)")
}

func snippet(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
