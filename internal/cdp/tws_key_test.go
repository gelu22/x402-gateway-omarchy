package cdp

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// 23.5: the whole point of keeping the scalar in our own buffer is that Wipe
// really overwrites it. big.Int cannot be erased through its API, so this test
// asserts BYTES, not Sign()==0 semantics.
func TestWalletSecretWipeZeroesEveryByte(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ws := NewWalletSecret("id-1", time.Time{}, []byte{1, 2, 3, 4}, key)

	if !ws.HasKey() {
		t.Fatal("HasKey() = false, want true for a live secret")
	}
	if len(ws.d) != p256ScalarLen {
		t.Fatalf("buffer length = %d, want %d", len(ws.d), p256ScalarLen)
	}
	want := key.D.FillBytes(make([]byte, p256ScalarLen))
	if !bytes.Equal(ws.d, want) {
		t.Fatal("buffer does not hold the private scalar")
	}
	if _, err := ws.XWalletAuth("PUT", "api.cdp.coinbase.com", "/platform/x", []byte(`{}`)); err != nil {
		t.Fatalf("sign before wipe: %v", err)
	}

	buf := ws.d // same backing array; survives ws.d = nil
	ws.Wipe()

	for i, b := range buf {
		if b != 0 {
			t.Fatalf("byte %d = %d after Wipe, want 0", i, b)
		}
	}
	if ws.HasKey() {
		t.Error("HasKey() = true after Wipe, want false")
	}
	if ws.PublicSPKI != nil {
		t.Error("PublicSPKI not cleared by Wipe")
	}
	if _, err := ws.XWalletAuth("PUT", "api.cdp.coinbase.com", "/platform/x", []byte(`{}`)); err == nil {
		t.Error("sign after Wipe: want error, got nil")
	}

	ws.Wipe() // idempotent, must not panic
}

func TestWalletSecretWithoutKey(t *testing.T) {
	ws := NewWalletSecret("id-2", time.Time{}, nil, nil)
	if ws.HasKey() {
		t.Error("HasKey() = true without a key")
	}
	if _, err := ws.ecdsaKey(); err == nil {
		t.Error("ecdsaKey() without a key: want error, got nil")
	}
}

func TestWalletSecretWipeNilReceiver(t *testing.T) {
	var ws *WalletSecret
	ws.Wipe() // must not panic
}

// A short scalar (< 32 bytes) must be left-padded so the rebuilt key matches.
func TestWalletSecretScalarPadding(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ws := NewWalletSecret("id-3", time.Time{}, nil, key)
	rebuilt, err := ws.ecdsaKey()
	if err != nil {
		t.Fatalf("ecdsaKey: %v", err)
	}
	if rebuilt.D.Cmp(key.D) != 0 {
		t.Error("rebuilt scalar differs from the original")
	}
	if rebuilt.X.Cmp(key.X) != 0 || rebuilt.Y.Cmp(key.Y) != 0 {
		t.Error("rebuilt public point differs from the original")
	}
}

// 25.1: mlock is best-effort (a low RLIMIT_MEMLOCK or a sandbox refuses it), so
// the assertions are about behaviour, not about the privilege being granted.
func TestWalletSecretWipeZeroesAndUnlocks(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ws := NewWalletSecret("id-lock", time.Time{}, []byte{9, 9}, key)
	t.Logf("mlock granted: %v", ws.Locked())

	ws.Wipe()
	if ws.HasKey() {
		t.Error("HasKey() = true after Wipe, want false")
	}
	if ws.Locked() {
		t.Error("Locked() = true after Wipe, want false (munlock)")
	}
	ws.Wipe() // idempotent, no double-munlock panic
}

// 25.1: the transient ecdsa key must not outlive the signature with a live
// scalar; wipeECDSAScalar zeroes the big.Int limbs in place.
func TestWipeECDSAScalarZeroesAllLimbs(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ws := NewWalletSecret("id-wipe", time.Time{}, nil, key)
	signing, err := ws.ecdsaKey()
	if err != nil {
		t.Fatal(err)
	}
	if signing.D.Sign() == 0 {
		t.Fatal("precondition: scalar is zero before the test")
	}
	limbs := signing.D.Bits()
	if len(limbs) == 0 {
		t.Fatal("precondition: no limbs to wipe")
	}

	wipeECDSAScalar(signing)

	for i, w := range limbs {
		if w != 0 {
			t.Fatalf("limb %d = %d after wipeECDSAScalar, want 0", i, w)
		}
	}
	if signing.D.Sign() != 0 {
		t.Error("Sign() != 0 after wipeECDSAScalar")
	}
	wipeECDSAScalar(nil) // nil-safe
}

// 25.1: Locked() must reflect the kernel's own counter, not our flag. Skip
// where mlock is refused (low RLIMIT_MEMLOCK, sandbox) — the skip is the honest
// outcome, a pass would be a lie.
func TestMlockReflectsInVmLck(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("VmLck is a Linux counter")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ws := NewWalletSecret("id-vmlck", time.Time{}, nil, key)
	if !ws.Locked() {
		t.Skip("mlock refused in this environment")
	}
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	var kb int
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "VmLck:") {
			if _, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "VmLck:")), "%d", &kb); err != nil {
				t.Fatalf("parse VmLck: %v", err)
			}
		}
	}
	if kb <= 0 {
		t.Fatalf("Locked() = true but VmLck = %d kB, want > 0", kb)
	}
	ws.Wipe()
}
