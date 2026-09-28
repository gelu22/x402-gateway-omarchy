//go:build linux

package main

import (
	"log/slog"

	"golang.org/x/sys/unix"
)

// hardenProcess reduces the attack surface of the long-running daemon, which
// holds the signing credential (TWS) and the session tokens in RAM.
//
// PR_SET_DUMPABLE=0 makes /proc/<pid>/mem, ptrace and core dumps unavailable to
// other processes of the same user — the cheapest real obstacle to dumping the
// key out of a running daemon. RLIMIT_CORE=0 stops the kernel writing cores.
//
// Best-effort by design: a sandbox/seccomp profile may reject the syscalls, and
// refusing to start would be worse than running without this extra layer. The
// failure is logged (never silent) and scripts/check-hardening.sh fails loudly
// when the protection is not active.
func hardenProcess(logger *slog.Logger) {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		logger.Warn("hardening: ptrace protection unavailable", "err", err)
	} else {
		logger.Info("hardening: ptrace/mem-read disabled")
	}
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}); err != nil {
		logger.Warn("hardening: core dump limit not set", "err", err)
	} else {
		logger.Info("hardening: core dumps disabled")
	}
}
