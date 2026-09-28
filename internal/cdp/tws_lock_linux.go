//go:build linux

package cdp

import "golang.org/x/sys/unix"

// excludeFromDump keeps the pinned scalar out of a core dump. RLIMIT_CORE=0
// already prevents the kernel writing cores (cmd/gateway/secure_linux.go); this
// is the second belt, independent of the process limit.
func excludeFromDump(b []byte) {
	_ = unix.Madvise(b, unix.MADV_DONTDUMP)
}
