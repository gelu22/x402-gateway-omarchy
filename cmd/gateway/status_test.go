package main

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestUsd(t *testing.T) {
	cases := map[int64]string{0: "0.00", 1: "0.00", 10_000: "0.01", 100: "0.00", 1_234_567: "1.23", 5_000_000: "5.00"}
	for micro, want := range cases {
		if got := usd(micro); got != want {
			t.Errorf("usd(%d) = %q, want %q", micro, got, want)
		}
	}
}

func TestTailFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "audit.log")
	content := "l1\n\nl2\nl3\nl4\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := tailFile(p, 2)
	if err != nil || len(lines) != 2 || lines[0] != "l3" || lines[1] != "l4" {
		t.Fatalf("tail = %v, %v; want [l3 l4], nil", lines, err)
	}
	if _, err := tailFile(filepath.Join(dir, "missing.log"), 10); err == nil {
		t.Fatal("want error for missing file, got nil")
	}
}

func TestReadPluginConfig(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cfgDir := filepath.Join(home, ".config", "omarchy", "x402-gateway")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// JSONC with comments, like the real panel-written file.
	raw := "// Payment network\n{\n\"paymentNetwork\": \"eip155:84532\",\n\"rememberedUrls\": [{\"url\": \"https://a.example/x\"}, {\"url\": \"https://b.example/y\"}]\n}\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	network, remembered, err := readPluginConfig(home)
	if err != nil || network != "eip155:84532" || remembered != 2 {
		t.Fatalf("got %q %d %v; want eip155:84532 2 nil", network, remembered, err)
	}
	if _, _, err := readPluginConfig(filepath.Join(dir, "nohome")); err == nil {
		t.Fatal("want error for missing config, got nil")
	}
	badDir := filepath.Join(dir, "badh")
	if err := os.MkdirAll(filepath.Join(badDir, ".config", "omarchy", "x402-gateway"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, ".config", "omarchy", "x402-gateway", "config.json"), []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readPluginConfig(badDir); err == nil {
		t.Fatal("want error for corrupt config, got nil")
	}
}

func TestFetchStatusDeadSocket(t *testing.T) {
	if _, err := fetchStatus(filepath.Join(t.TempDir(), "no.sock")); err == nil {
		t.Fatal("want error for dead socket, got nil")
	}
}

func TestFetchStatusNon200(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "gw.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	if _, err := fetchStatus(sock); err == nil {
		t.Fatal("want error for HTTP 500, got nil")
	}
}

func TestCheckSpendValid(t *testing.T) {
	dir := t.TempDir()
	if err := checkSpendValid(dir); err != nil {
		t.Fatalf("missing spend.json must pass, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spend.json"), []byte(`{"day":"2026-09-14"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkSpendValid(dir); err != nil {
		t.Fatalf("valid spend.json must pass, got %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "spend.json"), []byte(`{oops`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkSpendValid(dir); err == nil {
		t.Fatal("want error for corrupt spend.json, got nil")
	}
}

func TestTailFileTruncates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "audit.log")
	big := "x" + string(make([]byte, 2000)) + "y"
	for i := range []byte(big) {
		if big[i] == 0 {
			big = big[:i] + "z" + big[i+1:]
		}
	}
	content := "l1\n" + big + "\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := tailFile(p, 10)
	if err != nil || len(lines) != 2 {
		t.Fatalf("tail = %v, %v; want 2 lines, nil", lines, err)
	}
	if len(lines[1]) > 520 {
		t.Fatalf("line not truncated: %d chars", len(lines[1]))
	}
}

func TestFetchStatusLiveSocket(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "gw.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"active","new_future_field":1}`))
	}))
	st, err := fetchStatus(sock)
	if err != nil {
		t.Fatal(err)
	}
	if strField(st, "state") != "active" {
		t.Fatalf("state = %v, want active", st["state"])
	}
	if strField(st, "missing") != "" {
		t.Fatalf("missing field = %q, want empty (graceful degradation)", strField(st, "missing"))
	}
}
