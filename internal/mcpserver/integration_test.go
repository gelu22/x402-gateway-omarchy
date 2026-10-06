package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCallSocketFetchRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fetch":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":   200,
				"headers":  map[string]string{"Content-Type": "text/plain"},
				"body_b64": "",
			})
		case "/status":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version":        "test-v1",
				"wallet_address": "0x1234",
				"paused":         false,
			})
		case "/pause":
			var body map[string]bool
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]bool{"paused": body["paused"]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)

	// Test /fetch
	raw, err := callSocket(context.Background(), client, "", "/fetch", map[string]any{
		"url":    "https://example.com",
		"method": "GET",
	})
	if err != nil {
		t.Fatalf("callSocket /fetch: %v", err)
	}
	var fetchResult map[string]any
	if err := json.Unmarshal(raw, &fetchResult); err != nil {
		t.Fatalf("Unmarshal fetch: %v", err)
	}
	if fetchResult["status"] != float64(200) {
		t.Errorf("fetch status = %v, want 200", fetchResult["status"])
	}

	// Test /status (nil body → GET-style)
	raw, err = callSocket(context.Background(), client, "", "/status", nil)
	if err != nil {
		t.Fatalf("callSocket /status: %v", err)
	}
	var status map[string]any
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatalf("Unmarshal status: %v", err)
	}
	if status["version"] != "test-v1" {
		t.Errorf("status version = %v, want test-v1", status["version"])
	}

	// Test /pause
	raw, err = callSocket(context.Background(), client, "", "/pause", map[string]bool{"paused": true})
	if err != nil {
		t.Fatalf("callSocket /pause: %v", err)
	}
	var pauseResult map[string]bool
	if err := json.Unmarshal(raw, &pauseResult); err != nil {
		t.Fatalf("Unmarshal pause: %v", err)
	}
	if !pauseResult["paused"] {
		t.Errorf("pause = %v, want true", pauseResult["paused"])
	}
}

func TestCallSocketMalformedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)

	// callSocket doesn't parse JSON — it returns raw bytes
	raw, err := callSocket(context.Background(), client, "", "/test", nil)
	if err != nil {
		t.Fatalf("callSocket malformed: %v", err)
	}
	if !strings.Contains(string(raw), "{invalid json") {
		t.Errorf("raw = %q, want contains '{invalid json'", string(raw))
	}
}

func TestCallSocketEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)

	raw, err := callSocket(context.Background(), client, "", "/empty", nil)
	if err != nil {
		t.Fatalf("callSocket empty: %v", err)
	}
	if len(raw) != 0 {
		t.Errorf("raw len = %d, want 0", len(raw))
	}
}

func TestCallSocketUTF8Body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"greeting": "こんにちは"})
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)

	raw, err := callSocket(context.Background(), client, "", "/utf8", map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("callSocket UTF-8: %v", err)
	}
	if !strings.Contains(string(raw), "こんにちは") {
		t.Errorf("raw = %q, want contains 'こんにちは'", string(raw))
	}
}

func TestCallSocketSlowTimeout(t *testing.T) {
	// Use a port that's unlikely to be listening to trigger dial timeout
	// instead of hanging on a server that never responds
	client := &http.Client{
		Timeout: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := callSocket(ctx, client, "", "/timeout-test", nil)
	if err == nil {
		t.Fatal("callSocket timeout: want error, got nil")
	}
	if !strings.Contains(err.Error(), "gateway unreachable") {
		t.Errorf("error = %q, want contains 'gateway unreachable'", err.Error())
	}
}

func TestCallSocket402PaymentRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"x402Version": 2,
			"accepts":     []map[string]string{},
		})
	}))
	defer srv.Close()

	client := makeRedirectClient(srv.URL)

	_, err := callSocket(context.Background(), client, "", "/402", nil)
	if err == nil {
		t.Fatal("callSocket 402: want error, got nil")
	}
	if !strings.Contains(err.Error(), "gateway:") {
		t.Errorf("error = %q, want contains 'gateway:'", err.Error())
	}
	if !strings.Contains(err.Error(), "x402Version") {
		t.Errorf("error = %q, want contains 'x402Version'", err.Error())
	}
}
