package cdp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func hostOf(baseURL string) string {
	if u, err := url.Parse(baseURL); err == nil {
		return u.Host
	}
	return baseURL
}

// WalletSecret is a Temporary Wallet Secret (TWS): an ECDSA P-256 keypair whose
// private half never leaves the device. Recovered from @coinbase/cdp-core
// (index.web6.js createKeyPair + index.web85.js _doRefreshWalletSecret).
//
// The private scalar lives in the unexported buffer owned by this package (see
// tws_key.go) and the ecdsa key is rebuilt per signature — that is what makes
// Wipe real. Construct with NewWalletSecret; never copy the struct.
type WalletSecret struct {
	ID         string    `json:"wallet_secret_id"`
	ValidUntil time.Time `json:"valid_until"`
	PublicSPKI []byte    `json:"-"`

	// d is the P-256 private scalar (p256ScalarLen bytes, big-endian); never
	// serialized, wiped by Wipe.
	d []byte

	// locked is the mlock state of d, set only by NewWalletSecret and cleared
	// by Wipe (tws_key.go). Never serialized.
	locked bool
}

// CreateWalletSecret generates a fresh P-256 keypair locally and registers the
// public half (SPKI, base64) with CDP. Auth: end-user Bearer access token.
// Pass secretID to replace an existing TWS under the same identifier (rotation);
// empty string generates a random UUID.
func (c *Client) CreateWalletSecret(ctx context.Context, userID, accessToken string, validUntil time.Time, secretID string) (*WalletSecret, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("cdp: keygen: %w", err)
	}
	spk, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("cdp: spki marshal: %w", err)
	}
	if secretID == "" {
		if secretID, err = randomID(); err != nil {
			return nil, err
		}
	}
	body := map[string]string{
		"walletSecretId": secretID,
		"publicKey":      base64.StdEncoding.EncodeToString(spk),
		"validUntil":     validUntil.UTC().Format(time.RFC3339),
	}
	path := "/v2/embedded-wallet-api/end-users/" + userID + "/wallet-secrets"
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	tmp := NewWalletSecret("", time.Time{}, nil, key)
	defer tmp.Wipe()
	// The scalar now lives in tmp's buffer; the source key is no longer needed
	// on any exit path (the final copy at the return statement is evaluated
	// before deferred calls run). Same pattern as RenewWalletSecret below.
	defer wipeECDSAScalar(key)
	// JWT uris include the /platform base prefix (api-client URL.pathname).
	auth, err := tmp.XWalletAuth(http.MethodPut, hostOf(c.BaseURL), "/platform"+path, payload)
	if err != nil {
		return nil, err
	}
	raw, _, err := c.do(ctx, c.HTTP, http.MethodPut, path, body, map[string]string{
		"Authorization": "Bearer " + accessToken,
		"X-Wallet-Auth": auth,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		WalletSecretID string `json:"walletSecretId"`
		ValidUntil     string `json:"validUntil"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("cdp: tws decode: %w (%s)", err, truncate(raw))
	}
	vu, err := time.Parse(time.RFC3339, out.ValidUntil)
	if err != nil {
		// deliberate: a bogus server timestamp must not fail the whole
		// registration — the requested validUntil is the safe fallback, and a
		// wrong window only causes an early renewal (fail-closed on use).
		vu = validUntil
	}
	return NewWalletSecret(out.WalletSecretID, vu, spk, key), nil
}

// RenewWalletSecret re-registers an existing TWS under the SAME
// walletSecretId with the SAME public key and a new validUntil — the
// CDP-compliant renewal (verified live, S6). Unlike rotation, it does not
// consume a new TWS identity and cannot accumulate toward account limits.
func (c *Client) RenewWalletSecret(ctx context.Context, userID, accessToken string, ws *WalletSecret, validUntil time.Time) (*WalletSecret, error) {
	if !ws.HasKey() {
		return nil, fmt.Errorf("cdp: renew: missing wallet secret key")
	}
	key, err := ws.ecdsaKey()
	if err != nil {
		return nil, fmt.Errorf("cdp: renew: %w", err)
	}
	defer wipeECDSAScalar(key)
	spk, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("cdp: renew spki: %w", err)
	}
	body := map[string]string{
		"walletSecretId": ws.ID,
		"publicKey":      base64.StdEncoding.EncodeToString(spk),
		"validUntil":     validUntil.UTC().Format(time.RFC3339),
	}
	path := "/v2/embedded-wallet-api/end-users/" + userID + "/wallet-secrets"
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	// X-Wallet-Auth is signed by the SAME key being renewed (possession).
	auth, err := ws.XWalletAuth(http.MethodPut, hostOf(c.BaseURL), "/platform"+path, payload)
	if err != nil {
		return nil, err
	}
	raw, _, err := c.do(ctx, c.HTTP, http.MethodPut, path, body, map[string]string{
		"Authorization": "Bearer " + accessToken,
		"X-Wallet-Auth": auth,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		WalletSecretID string `json:"walletSecretId"`
		ValidUntil     string `json:"validUntil"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("cdp: renew decode: %w (%s)", err, truncate(raw))
	}
	vu, err := time.Parse(time.RFC3339, out.ValidUntil)
	if err != nil {
		// deliberate: a bogus server timestamp must not fail the whole
		// registration — the requested validUntil is the safe fallback, and a
		// wrong window only causes an early renewal (fail-closed on use).
		vu = validUntil
	}
	return NewWalletSecret(out.WalletSecretID, vu, spk, key), nil
}

// randomID returns a UUID-shaped identifier (SDK uses crypto.randomUUID()).
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("cdp: random: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}
