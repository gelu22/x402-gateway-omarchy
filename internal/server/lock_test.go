package server

import (
	"path/filepath"
	"testing"
)

// A second holder must fail while the first holds the lock; after release
// the path must be acquirable again (kernel releases flock on close/death).
func TestAcquireSingletonContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gw.sock.lock")

	release, err := AcquireSingleton(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := AcquireSingleton(path); err == nil {
		release()
		t.Fatal("second acquire while held must fail")
	}
	release()
	release2, err := AcquireSingleton(path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	release2()
}
