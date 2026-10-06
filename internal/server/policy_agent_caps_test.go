package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"gateway/internal/policy"
)

func TestPolicyAgentCapsBadLabelBeforeMFA(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: true, verified: true})
	body := bytes.NewReader([]byte(`{"agent_caps_micro_usdc":{"Bad":1000}}`))
	resp, err := testClient(socketPath).Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPolicyAgentCapsNegativeBeforeMFA(t *testing.T) {
	gw := newTestGateway(t)
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: true, verified: true})
	body := bytes.NewReader([]byte(`{"agent_daily_cap_micro_usdc":-1}`))
	resp, err := testClient(socketPath).Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestPolicyAgentCapsStaleLeavesFile(t *testing.T) {
	gw := newTestGateway(t)
	before, err := os.ReadFile(gw.PolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: true, verified: false})
	body := bytes.NewReader([]byte(`{"agent_daily_cap_micro_usdc":1000000}`))
	resp, err := testClient(socketPath).Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || errorCodeOf(t, resp) != "mfa_stale" {
		t.Fatalf("want 403 mfa_stale, got %d %s", resp.StatusCode, errorCodeOf(t, resp))
	}
	after, err := os.ReadFile(gw.PolicyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("policy.json must be unchanged on mfa_stale")
	}
}

func TestPolicyAgentCapsPOSTPersistsWithoutChangingDaily(t *testing.T) {
	gw := newTestGateway(t)
	dir := filepath.Dir(gw.PolicyPath)
	dailyBefore := gw.CurrentPolicy().DailyCapMicro
	socketPath := startTestServer(t, gw, &mockMFAServer{enrolled: true, verified: true})
	body := bytes.NewReader([]byte(
		`{"agent_daily_cap_micro_usdc":1000000,"agent_caps_micro_usdc":{"codex":200000}}`,
	))
	resp, err := testClient(socketPath).Post("http://localhost/policy", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, raw)
	}
	loaded, err := policy.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AgentDailyCapMicro != 1_000_000 || loaded.AgentCapMicro("codex") != 200_000 {
		t.Fatalf("persisted agent caps: default=%d codex=%d", loaded.AgentDailyCapMicro, loaded.AgentCapMicro("codex"))
	}
	if loaded.DailyCapMicro != dailyBefore {
		t.Fatalf("daily cap must be unchanged, got %d want %d", loaded.DailyCapMicro, dailyBefore)
	}
	if got := gw.CurrentPolicy().AgentDailyCapMicro; got != 1_000_000 {
		t.Fatalf("hot SetPolicy default = %d", got)
	}
}

func TestLogPolicyAgentCapsEmitsOnChange(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	if !logPolicyAgentCaps(logger, 0, 1_000_000, nil, map[string]int64{"codex": 1}) {
		t.Fatal("want emit")
	}
	var m map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &m); err != nil {
		t.Fatal(err)
	}
	if m["msg"] != "policy agent caps" {
		t.Fatalf("msg = %v", m["msg"])
	}
}

func TestLogPolicyAgentCapsSilentWhenEqual(t *testing.T) {
	caps := map[string]int64{"codex": 200_000}
	if logPolicyAgentCaps(slog.New(slog.NewJSONHandler(io.Discard, nil)), 1_000_000, 1_000_000, caps, caps) {
		t.Fatal("unchanged must not emit")
	}
}
