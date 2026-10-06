package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
)

// policyBody is the /policy POST body. Pointers distinguish "absent" from an
// explicit 0 (0 = always ask / feature off). AgentCapsMicro nil = leave map
// unchanged; non-nil (including empty) replaces the whole map.
type policyBody struct {
	DailyCapMicro       *int64           `json:"daily_cap_micro_usdc"`
	DomainSubCapPercent *int             `json:"domain_sub_cap_percent,omitempty"`
	AgentDailyCapMicro  *int64           `json:"agent_daily_cap_micro_usdc"`
	AgentCapsMicro      map[string]int64 `json:"agent_caps_micro_usdc"`
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
		if body.DailyCapMicro == nil && body.AgentDailyCapMicro == nil && body.AgentCapsMicro == nil {
			writeErr(w, http.StatusBadRequest, "bad_request",
				fmt.Errorf("daily_cap_micro_usdc or agent_* fields required"))
			return
		}
		// Range checks are client errors, so they answer 400 — and they run before
		// the sudo gate: asking for a TOTP code to then reject the value would
		// waste the user's verification window.
		current := s.Gateway.CurrentPolicy()
		updated := *current
		if body.DailyCapMicro != nil {
			updated.DailyCapMicro = *body.DailyCapMicro
		}
		if body.DomainSubCapPercent != nil {
			updated.DomainSubCapPercent = *body.DomainSubCapPercent
		}
		if body.AgentDailyCapMicro != nil {
			updated.AgentDailyCapMicro = *body.AgentDailyCapMicro
		}
		if body.AgentCapsMicro != nil {
			updated.AgentCapsMicro = copyAgentCaps(body.AgentCapsMicro)
		}
		if err := updated.Validate(); err != nil {
			writeErr(w, http.StatusBadRequest, "bad_request", err)
			return
		}
		if !s.requireSudoMFA(w) {
			return
		}
		if err := updated.Save(filepath.Dir(s.Gateway.PolicyPath)); err != nil {
			writeErr(w, http.StatusInternalServerError, "policy_save_failed", err)
			return
		}
		s.Gateway.SetPolicy(&updated)
		logPolicyCaps(s.AuditLogger,
			current.DailyCapMicro, updated.DailyCapMicro,
			current.DomainSubCapPercent, updated.DomainSubCapPercent)
		logPolicyAgentCaps(s.AuditLogger,
			current.AgentDailyCapMicro, updated.AgentDailyCapMicro,
			current.AgentCapsMicro, updated.AgentCapsMicro)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func copyAgentCaps(in map[string]int64) map[string]int64 {
	if in == nil {
		return nil
	}
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
