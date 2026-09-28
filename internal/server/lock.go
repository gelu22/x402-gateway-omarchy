// Single-instance guard: the daemon holds an exclusive flock on
// <socket>.lock for its whole lifetime (CONTRACTS §1 transport).
// A second instance exits before touching the socket or the session store.
// This matters: two live daemons race CDP refresh-token rotation, the loser
// gets its token rejected, and the session self-destructs (logout + Clear).
package server

import (
	"fmt"
	"os"
	"syscall"
)

// AcquireSingleton opens (0600) and exclusively flock-locks path in
// non-blocking mode. Returns a release func; the lock is also released
// automatically if the process dies (kernel closes the fd).
func AcquireSingleton(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600) // #nosec G304 -- path joins user-owned stateDir (same-user trust); no remote input
	if err != nil {
		return nil, fmt.Errorf("server: lock open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("server: locked by another gateway instance (%s): %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
