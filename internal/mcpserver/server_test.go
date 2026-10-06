package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCallSocketSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)
	raw, err := callSocket(context.Background(), client, "", "/test", map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("callSocket: %v", err)
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out["ok"] != "yes" {
		t.Errorf("response = %v, want {ok: yes}", out)
	}
}

func TestCallSocketNoBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"active"}`))
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)
	raw, err := callSocket(context.Background(), client, "", "/status", nil)
	if err != nil {
		t.Fatalf("callSocket: %v", err)
	}
	got := strings.TrimSpace(string(raw))
	want := `{"status":"active"}`
	if got != want {
		t.Errorf("response = %q, want %q", got, want)
	}
}

func TestCallSocketError400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad_request","detail":"invalid"}`))
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)
	_, err := callSocket(context.Background(), client, "", "/bad", nil)
	if err == nil {
		t.Fatal("callSocket 400: want error, got nil")
	}
	if !strings.Contains(err.Error(), "gateway:") {
		t.Errorf("error = %q, want contains 'gateway:'", err.Error())
	}
	if !strings.Contains(err.Error(), "bad_request") {
		t.Errorf("error = %q, want contains 'bad_request'", err.Error())
	}
}

func TestCallSocketError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server_error","detail":"internal"}`))
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)
	_, err := callSocket(context.Background(), client, "", "/error", nil)
	if err == nil {
		t.Fatal("callSocket 500: want error, got nil")
	}
	if !strings.Contains(err.Error(), "server_error") {
		t.Errorf("error = %q, want contains 'server_error'", err.Error())
	}
}

func TestCallSocketUnreachable(t *testing.T) {
	// Use a port that's unlikely to be listening
	client := &http.Client{Timeout: 100 * time.Millisecond}
	_, err := callSocket(context.Background(), client, "", "/test", nil)
	if err == nil {
		t.Fatal("callSocket unreachable: want error, got nil")
	}
	if !strings.Contains(err.Error(), "gateway unreachable") {
		t.Errorf("error = %q, want contains 'gateway unreachable'", err.Error())
	}
}

func TestCallSocketTimeout(t *testing.T) {
	// Use a port that's unlikely to be listening to trigger dial timeout
	client := &http.Client{
		Timeout: 100 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := callSocket(ctx, client, "", "/timeout-test", nil)
	if err == nil {
		t.Fatal("callSocket timeout: want error, got nil")
	}
	// Should be gateway unreachable (dial timeout)
	if !strings.Contains(err.Error(), "gateway unreachable") {
		t.Errorf("error = %q, want contains 'gateway unreachable'", err.Error())
	}
}

func TestCallSocketBodyWithSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"forbidden","detail":"secret leaked"}`))
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)
	_, err := callSocket(context.Background(), client, "", "/secret", map[string]string{"token": "super-secret-123"})
	if err == nil {
		t.Fatal("callSocket with body: want error, got nil")
	}
	// Error should contain the raw body (which includes the error detail)
	if !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("error = %q, want contains 'forbidden'", err.Error())
	}
}

func TestCallSocketMalformedJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)
	// Pass a body that will be JSON marshaled
	raw, err := callSocket(context.Background(), client, "", "/test", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("callSocket: %v", err)
	}
	if !strings.Contains(string(raw), `"ok":true`) {
		t.Errorf("response = %q, want contains {ok:true}", string(raw))
	}
}

// makeRedirectClient creates an http.Client that redirects "localhost" requests
// to the given test server URL (needed because callSocket hardcodes http://localhost).
func makeRedirectClient(targetURL string) *http.Client {
	targetHost := strings.TrimPrefix(targetURL, "http://")
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.Dial(network, targetHost)
			},
		},
	}
}

func TestStringsTrimSpace(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"hello", "hello"},
		{"  hello  ", "hello"},
		{"\thello\t", "hello"},
		{"\nhello\n", "hello"},
		{"\rhello\r", "hello"},
		{"  \t\n hello \n\t  ", "hello"},
		{"   ", ""},
		{"\t\n\r", ""},
	}
	for _, tt := range tests {
		got := stringsTrimSpace(tt.input)
		if got != tt.want {
			t.Errorf("stringsTrimSpace(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStringsReader(t *testing.T) {
	r := stringsReader("test")
	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != 4 || string(buf) != "test" {
		t.Errorf("Read() = %d, %q, want 4, 'test'", n, string(buf))
	}
}

func TestNew(t *testing.T) {
	// New() creates an MCP server — we can't easily test the tools without
	// a real MCP client, but we can verify it doesn't panic and returns a server.
	ctx := context.Background()
	srv, err := New(ctx, Options{
		SocketPath: "/tmp/test-mcp-socket-" + t.Name(),
		Version:    "test-v1",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv == nil {
		t.Fatal("New returned nil server")
	}
}

func TestNewNilContext(t *testing.T) {
	// New() should not panic with nil context (it stores it for later use)
	srv, err := New(nil, Options{
		SocketPath: "/tmp/test-mcp-nil-ctx",
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("New with nil ctx: %v", err)
	}
	if srv == nil {
		t.Fatal("New with nil ctx returned nil server")
	}
}

func TestTextResult(t *testing.T) {
	raw := []byte(`{"status":"ok"}`)
	result := textResult(raw)
	if result == nil {
		t.Fatal("textResult returned nil")
	}
	if len(result.Content) != 1 {
		t.Fatalf("textResult.Content length = %d, want 1", len(result.Content))
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("textResult.Content[0] type = %T, want *mcp.TextContent", result.Content[0])
	}
	if tc.Text != string(raw) {
		t.Errorf("TextContent.Text = %q, want %q", tc.Text, string(raw))
	}
}

func TestTextResultEmpty(t *testing.T) {
	raw := []byte{}
	result := textResult(raw)
	if result == nil {
		t.Fatal("textResult returned nil")
	}
	if len(result.Content) != 1 {
		t.Fatalf("textResult.Content length = %d, want 1", len(result.Content))
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("textResult.Content[0] type = %T, want *mcp.TextContent", result.Content[0])
	}
	if tc.Text != "" {
		t.Errorf("TextContent.Text = %q, want empty", tc.Text)
	}
}

// 34.5: RunStdio is the bridge's entry point (the only way agents spend money)
// and had 0% coverage. These tests use the real stdio transport with os.Pipe,
// no production seam.

// withStdioPipes swaps os.Stdin/os.Stdout for pipes and restores them after.
// Returns the write end the test sends into and the read end it reads from.
func withStdioPipes(t *testing.T) (in *os.File, out *os.File) {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldIn, oldOut
		_ = inR.Close()
		_ = inW.Close()
		_ = outR.Close()
		_ = outW.Close()
	})
	return inW, outR
}

// readLine reads one line from r, or fails after the timeout.
func readLine(t *testing.T, r *os.File) string {
	t.Helper()
	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(r)
		if sc.Scan() {
			lines <- sc.Text()
			return
		}
		lines <- ""
	}()
	select {
	case l := <-lines:
		return l
	case <-time.After(3 * time.Second):
		t.Fatal("no stdio response within 3s")
		return ""
	}
}

func TestRunStdioReturnsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunStdio(ctx, Options{SocketPath: "/tmp/nonexistent-mcp-" + t.Name()})
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunStdio did not return after context cancel")
	}
}

func TestRunStdioEmptyStdinExits(t *testing.T) {
	in, _ := withStdioPipes(t)
	_ = in.Close() // EOF on stdin: no client, nothing to serve

	done := make(chan error, 1)
	go func() { done <- RunStdio(context.Background(), Options{SocketPath: "/tmp/nonexistent-mcp"}) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RunStdio must exit on stdin EOF, not hang")
	}
}

// Full handshake: initialize, then a tools/call whose daemon socket does not
// exist must come back as an explicit error result, never a silent success.
func TestRunStdioHandshakeAndUnreachableDaemon(t *testing.T) {
	in, out := withStdioPipes(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunStdio(ctx, Options{SocketPath: "/tmp/nonexistent-mcp-" + t.Name()}) }()

	send := func(v any) {
		t.Helper()
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := in.Write(append(raw, '\n')); err != nil {
			t.Fatalf("write stdio: %v", err)
		}
	}
	send(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test", "version": "0"},
		},
	})
	if resp := readLine(t, out); !strings.Contains(resp, `"result"`) {
		t.Fatalf("initialize response = %q, want a result", resp)
	}
	send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	send(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{
			"name":      "fetch_with_payment",
			"arguments": map[string]any{"url": "https://example.com/x"},
		},
	})
	if resp := readLine(t, out); !strings.Contains(resp, "gateway unreachable") {
		t.Fatalf("tools/call response = %q, want the daemon-unreachable error", resp)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunStdio did not stop after cancel")
	}
}

// 54.1: Options.Agent is forwarded by tools via callSocket; empty = no header.
func TestOptionsAgentHeader(t *testing.T) {
	paths := []string{"/fetch", "/status", "/pause"}
	for _, agent := range []string{"opencode", ""} {
		agent := agent
		t.Run("agent="+agent, func(t *testing.T) {
			seen := map[string]string{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen[r.URL.Path] = r.Header.Get("X-Gateway-Agent")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			client := makeRedirectClient(srv.URL)
			opts := Options{Agent: agent}
			for _, p := range paths {
				if _, err := callSocket(context.Background(), client, opts.Agent, p, nil); err != nil {
					t.Fatalf("callSocket %s: %v", p, err)
				}
			}
			for _, p := range paths {
				got := seen[p]
				if agent == "" {
					if got != "" {
						t.Errorf("%s: header = %q, want absent", p, got)
					}
					continue
				}
				if got != agent {
					t.Errorf("%s: header = %q, want %q", p, got, agent)
				}
			}
		})
	}
}
