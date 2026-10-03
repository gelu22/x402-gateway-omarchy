// Socket handlers: status, pairing, fetch, MFA.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"gateway/internal/gateway"
)

// statusResponse is the /status envelope: the gateway status plus the list of
// payments that wait for the owner (49.2). Summaries only — never the blocked
// request's body or headers.
type statusResponse struct {
	*gateway.Status
	Blocked []BlockedSummary `json:"blocked,omitempty"`
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	st, err := s.Gateway.Status(s.Version)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "status_error", err)
		return
	}
	resp := statusResponse{Status: st}
	if s.blocked != nil {
		resp.Blocked = s.blocked.Summaries()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handlePairStatus(w http.ResponseWriter, _ *http.Request) {
	state, email, wallet := s.Pairing.PairState()
	writeJSON(w, http.StatusOK, map[string]string{
		"state":          state,
		"email":          email,
		"wallet_address": wallet,
	})
}

func (s *Server) handlePairInit(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var body struct {
		Email string `json:"email"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err)
		return
	}
	flowID, message, err := s.Pairing.InitPairing(ctx, body.Email)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "pair_init_error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"flowId":  flowID,
		"message": message,
	})
}

func (s *Server) handlePairVerify(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	var body struct {
		FlowID  string `json:"flowId"`
		FlowIDS string `json:"flow_id"`
		OTP     string `json:"otp"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err)
		return
	}
	flowID := body.FlowID
	if flowID == "" {
		flowID = body.FlowIDS
	}
	if flowID == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", errors.New("missing flowId"))
		return
	}
	if err := s.Pairing.VerifyPairing(ctx, flowID, body.OTP); err != nil {
		writeErr(w, http.StatusBadGateway, "pair_verify_error", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "active"})
}

func (s *Server) handlePairLogout(w http.ResponseWriter, _ *http.Request) {
	if err := s.Pairing.Logout(); err != nil {
		writeErr(w, http.StatusBadGateway, "pair_logout_error", err)
		return
	}
	if s.AuditLogger != nil {
		s.AuditLogger.Info("user logged out")
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "logged_out"})
}

func (s *Server) mapAndReply(w http.ResponseWriter, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
		return
	}
	// CONTRACTS §1: policy denials are 402, everything else 5xx.
	status := http.StatusBadGateway
	var perr *gateway.PolicyError
	if errors.As(err, &perr) {
		status = http.StatusPaymentRequired
	}
	writeErr(w, status, mapError(err), err)
}

// handlePause toggles gateway pause state.
func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paused bool `json:"paused"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err)
		return
	}
	prev := s.Gateway.Paused.Load()
	s.Gateway.Paused.Store(body.Paused)
	if prev != body.Paused && s.AuditLogger != nil {
		s.AuditLogger.Info("paused", "paused_old", prev, "paused_new", body.Paused)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"paused": s.Gateway.Paused.Load()})
}
