package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
)

// policyBody is the /policy POST body. Pointers distinguish "absent" from an
// explicit 0 (0 = always ask).
type policyBody struct {
	DailyCapMicro       *int64 `json:"daily_cap_micro_usdc"`
	DomainSubCapPercent *int   `json:"domain_sub_cap_percent,omitempty"`
}

// handlePolicy GET returns the active policy; POST updates and persists it
// (wizard budget step). Hot-reload picks the change up on the next fetch.
func (s *Server) handlePolicy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.Gateway.CurrentPolicy())
	case http.MethodPost:
		var body policyBody
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err)
			return
		}
		if body.DailyCapMicro == nil {
			writeErr(w, http.StatusBadRequest, "bad_request", fmt.Errorf("daily_cap_micro_usdc is required"))
			return
		}
		// Range checks are client errors, so they answer 400 — and they run before
		// the sudo gate: asking for a TOTP code to then reject the value would
		// waste the user's verification window. Fail-closed does not mean "every
		// rejection is a 5xx"; an I/O failure below is still a 500.
		// 0 is valid (auto-pay off), only negatives are not.
		if *body.DailyCapMicro < 0 {
			writeErr(w, http.StatusBadRequest, "bad_request",
				fmt.Errorf("daily_cap_micro_usdc must not be negative"))
			return
		}
		if body.DomainSubCapPercent != nil && (*body.DomainSubCapPercent < 0 || *body.DomainSubCapPercent > 100) {
			writeErr(w, http.StatusBadRequest, "bad_request",
				fmt.Errorf("domain_sub_cap_percent must be within 0..100"))
			return
		}
		// Raising the cap raises spending authority: require a fresh, CDP-attested
		// MFA verification before the change is persisted.
		if !s.requireSudoMFA(w) {
			return
		}
		current := s.Gateway.CurrentPolicy()
		updated := *current
		updated.DailyCapMicro = *body.DailyCapMicro
		// The sub-cap is config-file-only (no UI); preserve it unless posted.
		if body.DomainSubCapPercent != nil {
			updated.DomainSubCapPercent = *body.DomainSubCapPercent
		}
		if err := updated.Save(filepath.Dir(s.Gateway.PolicyPath)); err != nil {
			writeErr(w, http.StatusInternalServerError, "policy_save_failed", err)
			return
		}
		s.Gateway.SetPolicy(&updated)
		// Audit real changes only (011.2, file sink): GET/400/500 return above.
		logPolicyCaps(s.AuditLogger,
			current.DailyCapMicro, updated.DailyCapMicro,
			current.DomainSubCapPercent, updated.DomainSubCapPercent)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
