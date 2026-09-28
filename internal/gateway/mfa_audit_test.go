package gateway

import (
	"log/slog"
	"testing"
)

// TestMfaRequiredAuditLine proves that an MFA rejection produces exactly one
// audit line with outcome "failed:mfa_required", the correct amount_micro,
// and override == false (MFA is never overridable).
func TestMfaRequiredAuditLine(t *testing.T) {
	lines, raw := collectAudit(t, func(logger *slog.Logger) {
		LogPayment(logger, 10000, "https://seller.example/content", "failed:mfa_required", false)
	})
	if len(lines) != 1 {
		t.Fatalf("want 1 audit line, got %d", len(lines))
	}
	m := lines[0]
	assertAuditKeys(t, m)
	if m["amount_micro"] != float64(10000) {
		t.Fatalf("amount_micro = %v, want 10000", m["amount_micro"])
	}
	if m["domain"] != "seller.example" {
		t.Fatalf("domain = %v, want seller.example", m["domain"])
	}
	if m["outcome"] != "failed:mfa_required" {
		t.Fatalf("outcome = %v, want failed:mfa_required", m["outcome"])
	}
	if m["override"] != false {
		t.Fatalf("override = %v, want false", m["override"])
	}
	if raw == "" {
		t.Fatal("audit output must not be empty")
	}
}
