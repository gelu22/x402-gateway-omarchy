package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestHistoryLimitValidation(t *testing.T) {
	gw := newTestGateway(t)
	sock := startTestServer(t, gw, nil)
	c := testClient(sock)
	for _, q := range []string{"?limit=0", "?limit=-1", "?limit=abc"} {
		resp, err := c.Get("http://localhost/history" + q)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status=%d want 400", q, resp.StatusCode)
		}
		resp.Body.Close()
	}
	// Cap at 200: write 250 payment lines, ask for 500, get 200.
	audit := filepath.Join(filepath.Dir(sock), "audit.log")
	f, err := os.Create(audit)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 250; i++ {
		_, _ = f.WriteString(`{"time":"2026-01-01T00:00:00Z","msg":"payment audit","amount_micro":1,"domain":"x.example","outcome":"paid","override":false,"agent":""}` + "\n")
	}
	_ = f.Close()
	resp, err := c.Get("http://localhost/history?limit=500")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var body struct {
		Entries   []json.RawMessage `json:"entries"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 200 {
		t.Fatalf("entries=%d want 200", len(body.Entries))
	}
	if !body.Truncated {
		t.Fatal("want truncated")
	}
}

func TestHistoryMethodNotAllowed(t *testing.T) {
	sock := startTestServer(t, newTestGateway(t), nil)
	resp, err := testClient(sock).Post("http://localhost/history", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want 405", resp.StatusCode)
	}
}

func TestHistoryEmptyFile(t *testing.T) {
	sock := startTestServer(t, newTestGateway(t), nil)
	resp, err := testClient(sock).Get("http://localhost/history")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Entries   []any `json:"entries"`
		Truncated bool  `json:"truncated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Truncated || len(body.Entries) != 0 {
		t.Fatalf("body=%+v", body)
	}
}
