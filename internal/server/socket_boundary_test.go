package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// 35.3: socket input boundary. maxBodyBytes (socket.go:78) is enforced in five
// handlers but was pinned by no test: an oversized body must end in an explicit
// 4xx envelope, never a panic, never a silent success. Own file because
// socket_test.go is already 325 LOC and changed Go files are capped at 200.

// oversizedJSONBody is valid JSON that crosses the limit: the decoder has to
// read past maxBodyBytes while consuming the string value.
func oversizedJSONBody() string {
	return `{"url":"https://a.example/` + strings.Repeat("x", maxBodyBytes) + `"}`
}

func postJSON(t *testing.T, socketPath, path, body string) (*http.Response, string) {
	t.Helper()
	resp, err := testClient(socketPath).Post("http://localhost"+path, "application/json", strings.NewReader(body))
	if err != nil {
		// A handler panic closes the connection instead of answering.
		t.Fatalf("%s: transport error (handler panic?): %v", path, err)
	}
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("%s: read body: %v", path, err)
	}
	return resp, string(raw)
}

func TestFetchOversizedBodyRejected(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)

	resp, raw := postJSON(t, socketPath, "/fetch", oversizedJSONBody())
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, raw)
	}
	var env struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("body is not the JSON error envelope: %s", raw)
	}
	if env.Error != "bad_request" || !strings.Contains(env.Detail, "request body too large") {
		t.Fatalf("envelope = %+v, want bad_request + size-limit detail", env)
	}
}

func TestFetchOverrideOversizedBodyRejected(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)

	resp, raw := postJSON(t, socketPath, "/fetch-override", oversizedJSONBody())
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(raw, "request body too large") {
		t.Fatalf("body = %s, want the size-limit detail", raw)
	}
}

// The boundary itself: a body of exactly maxBodyBytes is accepted (only MORE is
// rejected) and follows the normal path — here: unreachable host → 502.
func TestFetchBodyAtLimitNotRejectedBySize(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)

	prefix := `{"url":"https://a.example/`
	body := prefix + strings.Repeat("x", maxBodyBytes-len(prefix)-2) + `"}`
	if len(body) != maxBodyBytes {
		t.Fatalf("fixture bug: body %d != limit %d", len(body), maxBodyBytes)
	}
	resp, raw := postJSON(t, socketPath, "/fetch", body)
	if strings.Contains(raw, "request body too large") {
		t.Fatalf("body under the limit was rejected by size: %s", raw)
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body accepted, fetch attempted): %s", resp.StatusCode, raw)
	}
}

// /policy POST has the same MaxBytesReader as the other five handlers
// (policy_endpoint.go:26, since 36.4): oversized bodies fail on the size
// limit, not on field validation.
func TestPolicyOversizedBodyRejected(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)

	resp, raw := postJSON(t, socketPath, "/policy", oversizedJSONBody())
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(raw, "request body too large") {
		t.Fatalf("body = %s, want the size-limit detail", raw)
	}
}

func TestFetchTruncatedJSON(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)

	resp, raw := postJSON(t, socketPath, "/fetch", `{"url":"https://a`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, raw)
	}
	if !strings.Contains(raw, `"error":"bad_request"`) {
		t.Fatalf("body = %s, want bad_request envelope", raw)
	}
}

func TestUnknownPathNotFound(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)

	resp, err := testClient(socketPath).Get("http://localhost/nope")
	if err != nil {
		t.Fatalf("transport error (handler panic?): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestKnownPathUnsupportedMethod(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	c := testClient(socketPath)

	resp, err := c.Get("http://localhost/fetch") // only POST is registered
	if err != nil {
		t.Fatalf("GET /fetch: transport error (handler panic?): %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /fetch status = %d, want 405", resp.StatusCode)
	}

	req, err := http.NewRequest(http.MethodDelete, "http://localhost/policy", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp2, err := c.Do(req)
	if err != nil {
		t.Fatalf("DELETE /policy: transport error (handler panic?): %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /policy status = %d, want 405", resp2.StatusCode)
	}
}

// FINDING F2 (for 35.6/Etap 36): /fetch does not require Content-Type; such a
// request follows the normal path. No boundary is crossed (socket 0700 +
// peercred, body still parsed as JSON), but the contract is unenforced. Pinned
// property either way: no silent success, always an explicit envelope.
func TestFetchMissingContentType(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, nil)
	c := testClient(socketPath)

	// (a) no header + no body → explicit 400, never a silent success
	resp, err := c.Post("http://localhost/fetch", "", strings.NewReader(""))
	if err != nil {
		t.Fatalf("empty body: transport error (handler panic?): %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(raw), `"error"`) {
		t.Fatalf("no header + empty body: status=%d body=%s, want 400 envelope", resp.StatusCode, raw)
	}

	// (b) no header + valid JSON → normal path, explicit error envelope
	resp2, err := c.Post("http://localhost/fetch", "", strings.NewReader(`{"url":"https://a.example"}`))
	if err != nil {
		t.Fatalf("valid body: transport error (handler panic?): %v", err)
	}
	raw2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode == http.StatusOK {
		t.Fatalf("no Content-Type must not become a silent success: %s", raw2)
	}
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw2, &env); err != nil || env.Error == "" {
		t.Fatalf("no header: body is not the JSON error envelope: %s", raw2)
	}
}
