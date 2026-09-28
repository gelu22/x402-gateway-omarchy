package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway/internal/gateway"
)

// Pins the pause audit through the real handler: transition shape,
// same-value silence, and nil-logger safety (minimal test Servers).
func TestHandlePauseAuditsFlip(t *testing.T) {
	post := func(srv *Server, paused bool) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]bool{"paused": paused})
		req := httptest.NewRequest(http.MethodPost, "/pause", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		srv.handlePause(rec, req)
		return rec
	}

	// flip false→true: exactly one line with old/new.
	var buf bytes.Buffer
	srv := &Server{Gateway: &gateway.Gateway{}, AuditLogger: slog.New(slog.NewJSONHandler(&buf, nil))}
	if rec := post(srv, true); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	lines, _ := decodePolicyAudit(t, buf.String())
	if len(lines) != 1 {
		t.Fatalf("want 1 line on flip, got %d", len(lines))
	}
	if lines[0]["msg"] != "paused" || lines[0]["paused_old"] != false || lines[0]["paused_new"] != true {
		t.Fatalf("wrong pause fields: %v", lines[0])
	}

	// re-POST same value: silence.
	buf.Reset()
	if rec := post(srv, true); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if buf.Len() != 0 {
		t.Fatalf("want silence on same-value, got %q", buf.String())
	}

	// nil logger on a real transition: no panic, 200, silence.
	srvNil := &Server{Gateway: &gateway.Gateway{}}
	if rec := post(srvNil, true); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
}
