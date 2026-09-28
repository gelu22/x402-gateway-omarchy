//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// unixPair returns a connected (client, server) unix socket pair, both closed
// at test end. SO_PEERCRED on the server side reports the client's process —
// here the test process itself.
func unixPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	accepted := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		accepted <- c
	}()

	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })

	select {
	case srv := <-accepted:
		t.Cleanup(func() { srv.Close() })
		return client, srv
	case err := <-errCh:
		t.Fatal(err)
	}
	return nil, nil
}

func TestPeerCredUnix(t *testing.T) {
	_, srv := unixPair(t)
	uid, pid, ok := peerCred(srv)
	if !ok {
		t.Fatal("want peer creds from a real unix connection")
	}
	if uid != os.Getuid() {
		t.Fatalf("uid = %d, want %d", uid, os.Getuid())
	}
	if pid <= 0 {
		t.Fatalf("pid = %d, want > 0", pid)
	}
}

func TestPeerCredNonUnixUnknown(t *testing.T) {
	// A non-unix conn must be reported as unknown, never panic (best-effort).
	uid, pid, ok := peerCred(deadConn{})
	if ok || uid != 0 || pid != 0 {
		t.Fatalf("want unknown, got uid=%d pid=%d ok=%v", uid, pid, ok)
	}
}

func TestLogPeerIncludesUidPid(t *testing.T) {
	_, srv := unixPair(t)
	var buf bytes.Buffer
	logPeer(slog.New(slog.NewJSONHandler(&buf, nil)), srv)

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("peer log not JSON: %v (%s)", err, buf.String())
	}
	if m["msg"] != "socket peer" {
		t.Fatalf("msg = %v", m["msg"])
	}
	if m["uid"] != float64(os.Getuid()) {
		t.Fatalf("uid = %v, want %d", m["uid"], os.Getuid())
	}
	if _, ok := m["pid"].(float64); !ok {
		t.Fatalf("pid missing/invalid: %v", m)
	}
}

// deadConn is a minimal net.Conn that is not a *net.UnixConn.
type deadConn struct{}

func (deadConn) Read([]byte) (int, error)         { return 0, os.ErrDeadlineExceeded }
func (deadConn) Write(b []byte) (int, error)      { return len(b), nil }
func (deadConn) Close() error                     { return nil }
func (deadConn) LocalAddr() net.Addr              { return nil }
func (deadConn) RemoteAddr() net.Addr             { return nil }
func (deadConn) SetDeadline(time.Time) error      { return nil }
func (deadConn) SetReadDeadline(time.Time) error  { return nil }
func (deadConn) SetWriteDeadline(time.Time) error { return nil }
