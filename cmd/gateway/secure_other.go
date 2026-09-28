//go:build !linux

package main

import "log/slog"

// hardenProcess is a no-op off Linux: PR_SET_DUMPABLE and RLIMIT_CORE handling
// here is Linux-specific. The release builds linux only; this stub keeps the
// package compilable on other platforms and still leaves an explicit trace
// (never a silent skip).
func hardenProcess(logger *slog.Logger) {
	logger.Warn("hardening: unsupported on this platform")
}
