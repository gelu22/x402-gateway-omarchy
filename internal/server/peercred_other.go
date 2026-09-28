//go:build !linux

package server

import "net"

// peerCred is a no-op off Linux: SO_PEERCRED is Linux-specific. Callers treat
// ok=false as "unknown" and never block (best-effort mitigation).
func peerCred(c net.Conn) (uid, pid int, ok bool) { return 0, 0, false }
