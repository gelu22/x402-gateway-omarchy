package cdp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"math/big"
	"time"

	"golang.org/x/sys/unix"
)

// p256ScalarLen is the fixed big-endian width of a P-256 private scalar. The
// scalar lives in a buffer of exactly this size so Wipe can overwrite every
// byte: a big.Int cannot be erased through its API (SetInt64/SetBytes only
// shorten the slice), which is why the key material is kept here instead.
const p256ScalarLen = 32

// NewWalletSecret holds a copy of key's private scalar in a buffer this package
// owns; the caller's key is not retained and may be dropped immediately. This
// is the only entry point for key material, so no code path can keep the scalar
// outside a buffer we are able to wipe.
func NewWalletSecret(id string, validUntil time.Time, publicSPKI []byte, key *ecdsa.PrivateKey) *WalletSecret {
	var d []byte
	if key != nil && key.D != nil {
		d = make([]byte, p256ScalarLen)
		raw := key.D.Bytes() // minimal big-endian, no leading zeros
		if len(raw) > p256ScalarLen {
			raw = raw[len(raw)-p256ScalarLen:]
		}
		copy(d[p256ScalarLen-len(raw):], raw)
	}
	ws := &WalletSecret{ID: id, ValidUntil: validUntil, PublicSPKI: publicSPKI, d: d}
	if len(d) == p256ScalarLen {
		ws.locked = lockBuffer(d)
	}
	return ws
}

// lockBuffer pins the scalar's pages in RAM (mlock) so the key cannot be
// written to swap, and excludes them from core dumps. Best-effort by design,
// same rule as cmd/gateway/secure_linux.go: a low RLIMIT_MEMLOCK or a sandbox
// may refuse the syscall, and failing to sign would be worse than running
// without this extra layer. The refusal is not silent — callers report it via
// WalletSecret.Locked().
func lockBuffer(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	if err := unix.Mlock(b); err != nil {
		return false
	}
	excludeFromDump(b)
	return true
}

// unlockBuffer releases the pin acquired by lockBuffer. Best-effort; nil-safe.
func unlockBuffer(b []byte) {
	if len(b) == 0 {
		return
	}
	_ = unix.Munlock(b)
}

// Locked reports whether the scalar buffer is currently pinned in RAM. False
// means the key is absent or mlock was refused — signing works either way.
func (ws *WalletSecret) Locked() bool { return ws != nil && ws.locked }

// wipeECDSAScalar zeroes the big.Int limbs of a signing key in place, then
// normalises the value. SetInt64(0) alone can leave limbs beyond the new
// length, so the slice behind Bits() is overwritten first. Copies the runtime
// makes inside ecdsa.Sign/ScalarBaseMult stay out of reach without unsafe
// (THREAT-MODEL T3).
func wipeECDSAScalar(k *ecdsa.PrivateKey) {
	if k == nil || k.D == nil {
		return
	}
	if w := k.D.Bits(); len(w) > 0 {
		for i := range w {
			w[i] = 0
		}
	}
	k.D.SetInt64(0)
}

// HasKey reports whether the private half is present (false after Wipe).
func (ws *WalletSecret) HasKey() bool { return ws != nil && len(ws.d) == p256ScalarLen }

// ecdsaKey rebuilds the signing key for a single operation. The returned key is
// transient: callers must not retain it, and its bytes are not erasable in Go
// (see THREAT-MODEL T3) — the durable copy is the buffer behind Wipe.
func (ws *WalletSecret) ecdsaKey() (*ecdsa.PrivateKey, error) {
	if !ws.HasKey() {
		return nil, errors.New("cdp: wallet secret key missing")
	}
	k := new(ecdsa.PrivateKey)
	k.Curve = elliptic.P256()
	k.D = new(big.Int).SetBytes(ws.d)
	k.X, k.Y = k.Curve.ScalarBaseMult(ws.d)
	if k.X == nil || k.Y == nil {
		return nil, errors.New("cdp: wallet secret key invalid")
	}
	return k, nil
}

// Wipe overwrites the private scalar and the public blob in place, then drops
// them: the secret is unusable afterwards. Idempotent and nil-safe; callers
// must not use the WalletSecret for signing after this.
func (ws *WalletSecret) Wipe() {
	if ws == nil {
		return
	}
	for i := range ws.d {
		ws.d[i] = 0
	}
	unlockBuffer(ws.d)
	ws.d = nil
	ws.locked = false
	for i := range ws.PublicSPKI {
		ws.PublicSPKI[i] = 0
	}
	ws.PublicSPKI = nil
}
