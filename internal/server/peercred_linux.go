//go:build linux

package server

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerCred reads SO_PEERCRED from a unix connection: the uid and pid of the
// process on the other end. Best-effort — ok=false when the connection is not
// a *net.UnixConn or the syscall fails (never blocks the accept path).
func peerCred(c net.Conn) (uid, pid int, ok bool) {
	uc, isUnix := c.(*net.UnixConn)
	if !isUnix {
		return 0, 0, false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, 0, false
	}
	var cred *unix.Ucred
	var sockErr error
	if cerr := raw.Control(func(fd uintptr) {
		cred, sockErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); cerr != nil || sockErr != nil || cred == nil {
		return 0, 0, false
	}
	return int(cred.Uid), int(cred.Pid), true
}
