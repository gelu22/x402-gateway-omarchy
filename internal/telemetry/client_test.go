package telemetry

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func newBackendT(t *testing.T, failFirst *atomic.Bool) (*httptest.Server, *atomic.Int64) {
	var accepted atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/devices/register":
			if failFirst.Swap(false) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{
				"device_id": "dev-1", "device_token": "tok-1",
			})
		case r.URL.Path == "/v1/telemetry/events":
			if failFirst.Load() && accepted.Load() == 0 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			accepted.Add(1)
			writeJSON(w, http.StatusOK, map[string]any{"accepted": 1})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &accepted
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestRegisterAndFlushHappyPath(t *testing.T) {
	fail := atomic.Bool{}
	srv, _ := newBackendT(t, &fail)
	dir := t.TempDir()

	c := New(srv.URL, dir)
	c.LoadQueue()
	c.Enqueue(Event{Type: "payment", AmountMicro: 10_000})

	if err := c.Register("cdp-access"); err != nil {
		t.Fatalf("register: %v", err)
	}
	n, err := c.Flush("")
	if err != nil || n != 1 {
		t.Fatalf("flush n=%d err=%v", n, err)
	}
	c.mu.Lock()
	left := len(c.queue)
	c.mu.Unlock()
	if left != 0 {
		t.Fatalf("queue not drained: %d", left)
	}
}

func TestOfflineEventsSurviveRestart(t *testing.T) {
	fail := atomic.Bool{}
	fail.Store(true)
	srv, _ := newBackendT(t, &fail)
	dir := t.TempDir()

	c := New(srv.URL, dir)
	c.Enqueue(Event{Type: "budget_exhausted"})

	// Simulate restart: fresh client over same state dir.
	c2 := New(srv.URL, dir)
	c2.LoadQueue()
	c2.mu.Lock()
	queued := len(c2.queue)
	c2.mu.Unlock()
	if queued != 1 {
		t.Fatalf("queue after restart = %d, want 1", queued)
	}
	fail.Store(false)
	n, err := c2.Flush("tok-1")
	if err != nil || n != 1 {
		t.Fatalf("flush after restart: n=%d err=%v", n, err)
	}
}

func TestDisabledClientIsNoOp(t *testing.T) {
	c := New("", t.TempDir())
	c.Enqueue(Event{Type: "payment"})
	if err := c.Register("x"); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.Flush(""); n != 0 {
		t.Fatal("disabled client must not flush")
	}
	if _, err := os.Stat(filepath.Join(c.StateDir, queueFileName)); !os.IsNotExist(err) {
		t.Fatal("queue file must not exist for disabled client")
	}
}

func TestTruncateEmpty(t *testing.T) {
	if got := truncate(nil); got != "" {
		t.Errorf("truncate(nil) = %q, want empty", got)
	}
	if got := truncate([]byte("")); got != "" {
		t.Errorf("truncate([]byte{}) = %q, want empty", got)
	}
}

func TestTruncateShort(t *testing.T) {
	input := []byte("hello world")
	if got := truncate(input); got != "hello world" {
		t.Errorf("truncate(11 bytes) = %q, want %q", got, "hello world")
	}
}

func TestTruncateExactly200(t *testing.T) {
	input := bytes.Repeat([]byte("a"), 200)
	if got := truncate(input); len(got) != 200 {
		t.Errorf("truncate(200 bytes) len = %d, want 200", len(got))
	}
	if got := truncate(input); got != string(input) {
		t.Errorf("truncate(200 bytes) changed content")
	}
}

func TestTruncateOver200(t *testing.T) {
	input := bytes.Repeat([]byte("a"), 300)
	got := truncate(input)
	if len(got) != 200 {
		t.Errorf("truncate(300 bytes) len = %d, want 200", len(got))
	}
	if got != string(bytes.Repeat([]byte("a"), 200)) {
		t.Errorf("truncate(300 bytes) content mismatch")
	}
}

func TestTruncateLarge(t *testing.T) {
	input := bytes.Repeat([]byte("x"), 10000)
	got := truncate(input)
	if len(got) != 200 {
		t.Errorf("truncate(10000 bytes) len = %d, want 200", len(got))
	}
}

func TestTruncateMultibyteUTF8(t *testing.T) {
	// "こんにちは" = 15 bytes (5 chars × 3 bytes each)
	input := []byte("こんにちは世界") // 18 bytes
	got := truncate(input)
	// Should cut at byte boundary, may produce invalid UTF-8
	if len(got) != len(input) {
		t.Errorf("truncate(18 bytes UTF-8) len = %d, want 18 (no cut)", len(got))
	}
	// Now truncate something larger
	long := bytes.Repeat([]byte("こんにちは"), 50) // 750 bytes
	got = truncate(long)
	if len(got) != 200 {
		t.Errorf("truncate(750 bytes UTF-8) len = %d, want 200", len(got))
	}
}

func TestRegisterStatusErrorTruncated(t *testing.T) {
	longBody := bytes.Repeat([]byte("e"), 500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write(longBody)
	}))
	defer srv.Close()

	c := New(srv.URL, t.TempDir())
	err := c.Register("token")
	if err == nil {
		t.Fatal("Register: want error, got nil")
	}
	errStr := err.Error()
	if len(errStr) > 250 {
		t.Errorf("error message len = %d, expected ~250 (prefix + truncated body)", len(errStr))
	}
}
