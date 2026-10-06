// Money audit trail (MUST logging): one structured line per payment attempt.
// Fields: amount/domain/outcome (+override flag, timestamp via slog).
// NEVER: full URLs, payloads, bodies, headers, secrets — domain only (T8-aligned).
//
// Production sink is audit.log (file-backed JSONL, durable, shell-independent):
// daemon stderr does NOT reach journald live (verified), so audit cannot rely
// on slog-to-stderr. main.go wires a file logger here; tests use buffers.
package gateway

import (
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

// MaxAuditBytes bounds audit.log growth (rotate on daemon start, keep one backup).
// Payments are human-scale; 10 MB ≈ 100k lines ≈ years.
const MaxAuditBytes = 10 << 20

// PaymentLine is one payment-audit row. Agent is a client declaration (may be empty).
type PaymentLine struct {
	AmountMicro int64
	Target      string
	Outcome     string
	Agent       string
	Override    bool
}

// LogPayment emits one audit line for a payment attempt. Nil-logger safe:
// Gateways built without a Logger (tests, minimal embeds) stay silent.
// Target is the full request URL; only its hostname is logged (T8).
func LogPayment(logger *slog.Logger, l PaymentLine) {
	if logger == nil {
		return
	}
	logger.Info("payment audit",
		"amount_micro", l.AmountMicro,
		"domain", auditDomain(l.Target),
		"outcome", l.Outcome,
		"override", l.Override,
		"agent", l.Agent,
	)
}

// auditDomain reduces a request URL to its hostname for logging.
// Unparsable input yields "" — never the raw URL, never a secret.
func auditDomain(rawTarget string) string {
	u, err := url.Parse(rawTarget)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// RotateAuditLog bounds audit growth: if path exceeds maxBytes, move it to
// path+".1" (clobbering any previous backup) so logging starts fresh.
func RotateAuditLog(path string, maxBytes int64) error {
	st, err := os.Stat(path)
	if err != nil || st.Size() <= maxBytes {
		return nil
	}
	return os.Rename(path, path+".1")
}

// policyOutcome allowlists denial codes into audit outcomes (policy checks
// plus the mfa_required signing gate).
// Unknown codes fall back to a generic denial — never a raw error string,
// which a future Check edit could lace with seller payload.
func policyOutcome(code string) string {
	switch code {
	case "invalid_amount", "budget_exceeded", "network_denied", "price_changed", "unknown_seller", "domain_cap_exceeded", "agent_cap_exceeded", "mfa_required", "policy_violation", "insufficient_funds":
		return "failed:" + code
	default:
		return "failed:policy_denied"
	}
}

// auditErrorCode maps a doFetch terminal error to a stable audit code.
// Mirrors server.mapError's non-PolicyError vocabulary (short lists, reviewed
// together; a shared helper would couple the packages for 6 lines).
// An unclassified internal error is server_error, never a misleading
// upstream_error; real upstream failures match ErrUpstream explicitly.
func auditErrorCode(err error) string {
	var perr *PolicyError
	if errors.As(err, &perr) {
		return strings.TrimPrefix(policyOutcome(perr.Code), "failed:")
	}
	switch {
	case errors.Is(err, ErrPaused):
		return "paused"
	case errors.Is(err, ErrDuplicate):
		return "duplicate_payment"
	case errors.Is(err, ErrBadTarget):
		return "bad_target"
	case errors.Is(err, ErrSigner):
		return "signer_error"
	case errors.Is(err, ErrNoRequirements):
		return "no_requirements"
	case errors.Is(err, ErrContentTooLarge):
		return "content_too_large"
	case errors.Is(err, ErrUpstream):
		return "upstream_error"
	default:
		return "server_error"
	}
}
