package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// collectAudit runs f against a JSON buffer logger, returning decoded lines
// plus the raw text (for NEVER leak assertions on the exact emitted bytes).
func collectAudit(t *testing.T, f func(*slog.Logger)) ([]map[string]any, string) {
	t.Helper()
	var buf bytes.Buffer
	f(slog.New(slog.NewJSONHandler(&buf, nil)))
	return decodeAuditLines(t, buf.String())
}

// decodeAuditLines parses JSONL audit output. Shared by helper tests and
// integration tests that own their buffer (e.g. paused-fetch via Gateway).
func decodeAuditLines(t *testing.T, raw string) ([]map[string]any, string) {
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

// assertAuditKeys guards NEVER at the shape level: only allowlisted keys
// may appear, so a future field cannot silently leak a URL, body or secret.
func assertAuditKeys(t *testing.T, m map[string]any) {
	t.Helper()
	allow := map[string]bool{
		"time": true, "level": true, "msg": true,
		"amount_micro": true, "domain": true,
		"outcome": true, "override": true, "agent": true,
	}
	for k := range m {
		if !allow[k] {
			t.Fatalf("unexpected audit key %q (possible leak)", k)
		}
	}
	if m["msg"] != "payment audit" {
		t.Fatalf("want msg %q, got %v", "payment audit", m["msg"])
	}
	if _, ok := m["time"].(string); !ok {
		t.Fatal("want RFC3339 time string")
	}
}

func TestLogPaymentPaid(t *testing.T) {
	lines, raw := collectAudit(t, func(l *slog.Logger) {
		LogPayment(l, PaymentLine{AmountMicro: 100000, Target: "https://seller.example/paid-article?x=1", Outcome: "paid"})
	})
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	m := lines[0]
	assertAuditKeys(t, m)
	if m["amount_micro"] != float64(100000) ||
		m["domain"] != "seller.example" || m["outcome"] != "paid" || m["override"] != false {
		t.Fatalf("wrong fields: %v", m)
	}
	if strings.Contains(raw, "paid-article") || strings.Contains(raw, "?x=1") {
		t.Fatalf("path/query leaked into audit: %s", raw)
	}
}

func TestLogPaymentFailedOverride(t *testing.T) {
	lines, raw := collectAudit(t, func(l *slog.Logger) {
		LogPayment(l, PaymentLine{AmountMicro: 250000, Target: "https://api.example/crawl?q=2", Outcome: "failed:budget_exceeded", Override: true})
	})
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	m := lines[0]
	assertAuditKeys(t, m)
	if m["outcome"] != "failed:budget_exceeded" || m["override"] != true ||
		m["domain"] != "api.example" {
		t.Fatalf("wrong fields: %v", m)
	}
	if strings.Contains(raw, "/crawl") || strings.Contains(raw, "?q=2") {
		t.Fatalf("path/query leaked into audit: %s", raw)
	}
}

func TestLogPaymentUnparsableTarget(t *testing.T) {
	lines, _ := collectAudit(t, func(l *slog.Logger) {
		LogPayment(l, PaymentLine{Outcome: "failed:bad_request"})
	})
	if len(lines) != 1 {
		t.Fatalf("want 1 line (never silent), got %d", len(lines))
	}
	if lines[0]["domain"] != "" || lines[0]["amount_micro"] != float64(0) {
		t.Fatalf("want empty domain + zero amount: %v", lines[0])
	}
}

func TestLogPaymentNeverLeaksSecrets(t *testing.T) {
	_, raw := collectAudit(t, func(l *slog.Logger) {
		LogPayment(l, PaymentLine{AmountMicro: 5000, Target: "https://pay.example/item?token=S3CR3T&key=abc#frag", Outcome: "paid"})
	})
	for _, secret := range []string{"S3CR3T", "abc", "token=", "key=", "#frag", "/item"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("secret/path fragment %q leaked into audit: %s", secret, raw)
		}
	}
}

func TestLogPaymentNilLoggerSilent(t *testing.T) {
	// Must neither panic nor emit: minimal Gateways without a logger stay quiet.
	LogPayment(nil, PaymentLine{AmountMicro: 1, Target: "https://x.example/", Outcome: "paid"})
}

func TestPolicyOutcomeAllowlist(t *testing.T) {
	for _, code := range []string{"invalid_amount", "budget_exceeded", "network_denied", "unknown_seller", "domain_cap_exceeded", "mfa_required"} {
		if got := policyOutcome(code); got != "failed:"+code {
			t.Fatalf("want passthrough for %q, got %q", code, got)
		}
	}
	for _, evil := range []string{"", "budget_exceeded: seller says <payload>", "402 Payment Required: <html>"} {
		if got := policyOutcome(evil); got != "failed:policy_denied" {
			t.Fatalf("want fallback for %q, got %q", evil, got)
		}
	}
}

func TestRotateAuditLog(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "audit.log")
	// Absent → nil no-op.
	if err := RotateAuditLog(p, 10); err != nil {
		t.Fatalf("absent: %v", err)
	}
	// Under cap → untouched, no backup.
	if err := os.WriteFile(p, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RotateAuditLog(p, 10); err != nil {
		t.Fatalf("under cap: %v", err)
	}
	if _, err := os.Stat(p + ".1"); !os.IsNotExist(err) {
		t.Fatal("under cap must not rotate")
	}
	// Over cap → moved to .1, path freed for a fresh open.
	if err := os.WriteFile(p, []byte("12345678901"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RotateAuditLog(p, 10); err != nil {
		t.Fatalf("over cap: %v", err)
	}
	b, err := os.ReadFile(p + ".1")
	if err != nil || string(b) != "12345678901" {
		t.Fatalf("backup wrong: %q %v", b, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("path must be freed after rotate")
	}
}

func TestRotateAuditLogEdges(t *testing.T) {
	dir := t.TempDir()
	// Exact boundary (size == cap) → no-op, no backup.
	p := filepath.Join(dir, "exact.log")
	if err := os.WriteFile(p, []byte("1234567890"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RotateAuditLog(p, 10); err != nil {
		t.Fatalf("boundary: %v", err)
	}
	if _, err := os.Stat(p + ".1"); !os.IsNotExist(err) {
		t.Fatal("boundary must not rotate")
	}
	// Stale .1 backup is clobbered with the new content.
	q := filepath.Join(dir, "clobber.log")
	if err := os.WriteFile(q+".1", []byte("OLD"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(q, []byte("12345678901"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RotateAuditLog(q, 10); err != nil {
		t.Fatalf("clobber: %v", err)
	}
	if b, _ := os.ReadFile(q + ".1"); string(b) != "12345678901" {
		t.Fatalf("backup not overwritten: %q", b)
	}
	// Rename failure (non-empty .1 dir) propagates so main can warn.
	r := filepath.Join(dir, "norename.log")
	if err := os.Mkdir(r+".1", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r+".1/sentinel", []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r, []byte("12345678901"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RotateAuditLog(r, 10); err == nil {
		t.Fatal("want rename error, got nil")
	}
}

func TestAuditErrorCode(t *testing.T) {
	cases := []struct {
		want string
		err  error
	}{
		{"paused", ErrPaused},
		{"duplicate_payment", ErrDuplicate},
		{"bad_target", ErrBadTarget},
		{"signer_error", ErrSigner},
		{"no_requirements", ErrNoRequirements},
		{"upstream_error", ErrUpstream},
		{"server_error", errors.New("boom")},
	}
	for _, c := range cases {
		if got := auditErrorCode(c.err); got != c.want {
			t.Errorf("want %q, got %q (%v)", c.want, got, c.err)
		}
	}
	if got := auditErrorCode(&PolicyError{Code: "budget_exceeded"}); got != "budget_exceeded" {
		t.Errorf("want PolicyError passthrough, got %q", got)
	}
	if got := auditErrorCode(&PolicyError{Code: "price_changed"}); got != "price_changed" {
		t.Errorf("want price_changed passthrough, got %q", got)
	}
	if got := auditErrorCode(&PolicyError{Code: "weird <payload>"}); got != "policy_denied" {
		t.Errorf("want fallback, got %q", got)
	}
}

func TestPausedFetchAuditsOnce(t *testing.T) {
	// Proves the defer backstop: a pre-pricing failure emits exactly one line
	// with honest zeros (no amount/bucket exists), without touching network.
	gw, _ := newGateway(t, 1000000, 250000)
	var buf bytes.Buffer
	gw.Logger = slog.New(slog.NewJSONHandler(&buf, nil))
	gw.Paused.Store(true)
	_, err := gw.Fetch(context.Background(), "GET", "https://pay.example/item?x=1", nil, nil)
	if !errors.Is(err, ErrPaused) {
		t.Fatalf("want ErrPaused, got %v", err)
	}
	lines, raw := decodeAuditLines(t, buf.String())
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 audit line, got %d", len(lines))
	}
	m := lines[0]
	assertAuditKeys(t, m)
	if m["outcome"] != "failed:paused" || m["amount_micro"] != float64(0) ||
		m["domain"] != "pay.example" || m["override"] != false {
		t.Fatalf("wrong fields: %v", m)
	}
	if strings.Contains(raw, "/item") || strings.Contains(raw, "?x=1") {
		t.Fatalf("path/query leaked: %s", raw)
	}
}

// 36.3: one seller denial = exactly one audit line, carrying the real amount.
// (Before: evaluateSeller logged the denial AND the doFetch block logged it
// again with amount 0.)
func TestUnknownSellerDenialAuditsExactlyOnce(t *testing.T) {
	gw, _ := newSettleGateway(t)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	url := sellerWith(t, http.StatusOK).URL + "/content"

	lines, _ := collectAudit(t, func(l *slog.Logger) {
		gw.Logger = l
		_, _ = gw.Fetch(context.Background(), http.MethodGet, url, nil, nil)
	})
	if len(lines) != 1 {
		t.Fatalf("audit lines = %d, want exactly 1 per denial", len(lines))
	}
	m := lines[0]
	if m["outcome"] != "failed:unknown_seller" || m["amount_micro"] != float64(10_000) {
		t.Fatalf("wrong audit line: %v", m)
	}
	if e := gw.lastError(); e == nil || e.Code != "unknown_seller" || e.AmountMicro != 10_000 {
		t.Fatalf("last_fetch_error = %+v, want unknown_seller with 10000", e)
	}
}
