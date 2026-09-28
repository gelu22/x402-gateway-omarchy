package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// decodePolicyAudit parses JSONL audit output into decoded lines + raw text.
func decodePolicyAudit(t *testing.T, raw string) ([]map[string]any, string) {
	t.Helper()
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ""
	}
	var out []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("audit line not JSON: %v", err)
		}
		out = append(out, m)
	}
	return out, raw
}

func policyAuditLogged(t *testing.T, oldCap, newCap int64, oldSub, newSub int) ([]map[string]any, string) {
	t.Helper()
	var buf bytes.Buffer
	logPolicyCaps(slog.New(slog.NewJSONHandler(&buf, nil)), oldCap, newCap, oldSub, newSub)
	return decodePolicyAudit(t, buf.String())
}

func assertPolicyKeys(t *testing.T, m map[string]any) {
	t.Helper()
	allow := map[string]bool{
		"time": true, "level": true, "msg": true,
		"cap_old_micro": true, "cap_new_micro": true,
		"subcap_old_percent": true, "subcap_new_percent": true,
	}
	for k := range m {
		if !allow[k] {
			t.Fatalf("unexpected audit key %q", k)
		}
	}
	if m["msg"] != "policy caps" {
		t.Fatalf("want msg %q, got %v", "policy caps", m["msg"])
	}
}

func TestLogPolicyCapsChange(t *testing.T) {
	lines, _ := policyAuditLogged(t, 2_000_000, 3_000_000, 20, 20)
	if len(lines) != 1 {
		t.Fatalf("want 1 line on change, got %d", len(lines))
	}
	m := lines[0]
	assertPolicyKeys(t, m)
	if m["cap_old_micro"] != float64(2_000_000) || m["cap_new_micro"] != float64(3_000_000) {
		t.Fatalf("wrong fields: %v", m)
	}
}

func TestLogPolicyCapsSubCapOnly(t *testing.T) {
	lines, _ := policyAuditLogged(t, 2_000_000, 2_000_000, 20, 35)
	if len(lines) != 1 {
		t.Fatalf("want 1 line on sub-cap-only change, got %d", len(lines))
	}
	assertPolicyKeys(t, lines[0])
	if lines[0]["subcap_old_percent"] != float64(20) || lines[0]["subcap_new_percent"] != float64(35) {
		t.Fatalf("wrong sub-cap fields: %v", lines[0])
	}
}

func TestLogPolicyCapsNoopSilent(t *testing.T) {
	lines, raw := policyAuditLogged(t, 2_000_000, 2_000_000, 20, 20)
	if len(lines) != 0 || raw != "" {
		t.Fatalf("want silence on no-op, got %d lines: %s", len(lines), raw)
	}
}

func TestLogPolicyCapsNilSilent(t *testing.T) {
	// Minimal Servers without an audit logger stay quiet, never panic.
	if logPolicyCaps(nil, 1, 2, 20, 20) {
		t.Fatal("nil logger must not report logged")
	}
}
