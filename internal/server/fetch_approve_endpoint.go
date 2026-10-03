// POST /fetch-approve (49.4-daemon half): replay a payment the daemon refused
// and kept in the blocked store, on the owner's explicit approval.
//
// "Approve" means "pay now": the daemon has the method/URL/body/headers it
// remembered, so it re-runs the payment with the recorded amount as the ceiling.
// The owner accepts that the content may go unused if the agent has moved on
// (decision A). This raises spending authority, so it goes through sudo.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *Server) handleFetchApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		s.handleFetchDismiss(w, r)
		return
	}
	ctx := r.Context()
	var body struct {
		ID string `json:"id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", fmt.Errorf("id is required"))
		return
	}
	if s.blocked == nil {
		writeErr(w, http.StatusNotFound, "not_found", fmt.Errorf("no blocked payments"))
		return
	}
	br, ok := s.blocked.Get(body.ID)
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", fmt.Errorf("blocked payment not found (expired or already handled)"))
		return
	}
	// Pays from the wallet: fresh MFA before anything is signed.
	if !s.requireSudoMFA(w) {
		return
	}
	method := br.Method
	if method == "" {
		method = http.MethodGet
	}
	// approveSeller=true lands the seller (TOFU); the recorded amount is the
	// ceiling, so a raised price surfaces as price_changed instead of overpaying.
	result, err := s.Gateway.FetchWithOverride(ctx, method, br.URL, br.Body, br.Headers, br.AmountMicro, true)
	if err != nil {
		s.mapAndReply(w, err)
		return
	}
	s.blocked.Remove(br.ID) // paid: the entry is done
	writeJSON(w, http.StatusOK, result)
}

// handleFetchDismiss drops a blocked entry without paying. Dismissing a notice
// is not a spending action, so it needs no sudo.
func (s *Server) handleFetchDismiss(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" || s.blocked == nil || !s.blocked.Remove(id) {
		writeErr(w, http.StatusNotFound, "not_found", fmt.Errorf("blocked payment not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
