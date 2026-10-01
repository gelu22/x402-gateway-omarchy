// x402 signing: BuildAuthorization, SignAuthorizationViaCDP, EncodePaymentSignatureHeader.
package x402

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gateway/internal/cdp"
)

const x402Version = 2

// Authorization is the EIP-3009 transferWithAuthorization message.
type Authorization struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Value       string `json:"value"`
	ValidAfter  string `json:"validAfter"`
	ValidBefore string `json:"validBefore"`
	Nonce       string `json:"nonce"`
}

// BuildAuthorization prepares the EIP-3009 message per @x402/evm
// createEIP3009Payload: validAfter=0, validBefore=now+maxTimeoutSeconds.
func BuildAuthorization(from string, req *PaymentRequirements) (*Authorization, error) {
	if !isHexAddress(from) {
		return nil, fmt.Errorf("x402: signer address %q is not a hex address", from)
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	timeout := req.MaxTimeoutSeconds
	if timeout <= 0 {
		return nil, fmt.Errorf("x402: maxTimeoutSeconds must be positive")
	}
	// Defense in depth: selection clamps, but BuildAuthorization must stay
	// safe for any caller (no overflow, no absurd validity window).
	if timeout > MaxAuthorizationTTLSeconds {
		timeout = MaxAuthorizationTTLSeconds
	}
	return &Authorization{
		From:        strings.ToLower(from),
		To:          strings.ToLower(req.PayTo),
		Value:       req.Amount,
		ValidAfter:  "0",
		ValidBefore: fmt.Sprint(now + int64(timeout)),
		Nonce:       "0x" + hex.EncodeToString(nonce),
	}, nil
}

// SignAuthorizationViaCDP asks CDP to sign the EIP-712 typed data with the end
// user's EVM account (TWS-authenticated), returning the 0x-prefixed signature.
func SignAuthorizationViaCDP(ctx context.Context, client *cdp.Client, ws *cdp.WalletSecret, userID, accessToken, evmAddress string, chainID int64, req *PaymentRequirements, auth *Authorization) (string, error) {
	td, err := buildTypedData(chainID, req, auth)
	if err != nil {
		return "", err
	}
	tdJSON, err := json.Marshal(td)
	if err != nil {
		return "", err
	}
	// RawMessage embeds the already-marshalled typed data verbatim — no
	// decode/re-encode round-trip, no `any` that could silently turn the
	// object into a string (47.4).
	body := map[string]any{
		"address":        evmAddress,
		"typedData":      json.RawMessage(tdJSON),
		"walletSecretId": ws.ID,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	path := "/v2/embedded-wallet-api/end-users/" + userID + "/evm/sign/typed-data"
	fullPath := path + "?projectID=" + url.QueryEscape(client.ProjectID)
	jwt, err := ws.XWalletAuth(http.MethodPost, cdp.APIHost, "/platform"+path, payload)
	if err != nil {
		return "", err
	}
	raw, _, err := client.PostJSON(ctx, fullPath, payload, map[string]string{
		"Authorization": "Bearer " + accessToken,
		"X-Wallet-Auth": jwt,
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("x402: sign decode: %w", err)
	}
	if !strings.HasPrefix(out.Signature, "0x") || len(out.Signature) != 132 {
		return "", fmt.Errorf("x402: unexpected signature shape (len %d)", len(out.Signature))
	}
	return out.Signature, nil
}

// EncodePaymentSignatureHeader builds the v2 wire payload and encodes it as
// the base64 PAYMENT-SIGNATURE header value (@x402/core encodePaymentSignatureHeader).
// extensions, when non-nil, is merged as the top-level "extensions" object
// (e.g. {"builder-code": {...}}); nil keeps the wire identical to no-extension.
func EncodePaymentSignatureHeader(resource map[string]any, accepted *PaymentRequirements, auth *Authorization, signature string, extensions map[string]any) (string, error) {
	wire := map[string]any{
		"x402Version": x402Version,
		"accepted":    accepted,
		"payload": map[string]any{
			"authorization": auth,
			"signature":     signature,
		},
	}
	if len(extensions) > 0 {
		wire["extensions"] = extensions
	}
	if resource != nil {
		wire["resource"] = resource
	}
	buf, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}
