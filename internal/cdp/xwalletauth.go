package cdp

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// XWalletAuth builds the X-Wallet-Auth JWT that authenticates end-user wallet
// operations (TWS registration, signing). Contract recovered from
// @coinbase/cdp-core index.web85.js getXWalletAuth:
//
//	header {alg:ES256, typ:JWT}
//	claims {uris:["METHOD hostpath"], reqHash?:sha256hex(body), iat, nbf, jti}
//	signature: ECDSA P-256, JOSE r||s encoding
//
// The server verifies the token with the public key registered in the same
// request (proof of possession of the TWS private half).
func (ws *WalletSecret) XWalletAuth(method, host, path string, body []byte) (string, error) {
	now := time.Now().Unix()
	jti, err := randomJTI()
	if err != nil {
		return "", err
	}
	claims := map[string]any{
		"uris": []string{method + " " + host + path},
		"iat":  now,
		"nbf":  now,
		"jti":  jti,
	}
	if len(body) > 0 {
		sum := sha256.Sum256(body)
		claims["reqHash"] = hexString(sum[:])
	}
	return ws.signJWT(claims)
}

func (ws *WalletSecret) signJWT(claims map[string]any) (string, error) {
	header := map[string]string{"alg": "ES256", "typ": "JWT"}
	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64URL(hb) + "." + base64URL(cb)
	sum := sha256.Sum256([]byte(signingInput))
	// The key is rebuilt for this signature only; the durable copy of the
	// scalar stays in the wipeable buffer (tws_key.go).
	key, err := ws.ecdsaKey()
	if err != nil {
		return "", err
	}
	// Zero the transient scalar's limbs on every exit path, including a failed
	// signature; the durable copy stays in the wipeable buffer.
	defer wipeECDSAScalar(key)
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", fmt.Errorf("cdp: jwt sign: %w", err)
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signingInput + "." + base64URL(sig), nil
}

func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}

// randomJTI returns a compact unique token ID; errors propagate instead of
// panicking (consistent with the package's error-returning style).
func randomJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("cdp: random: %w", err)
	}
	var x big.Int
	x.SetBytes(b)
	return x.Text(36), nil
}
