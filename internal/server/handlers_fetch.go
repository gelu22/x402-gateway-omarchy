// Fetch handlers: /fetch and /fetch-override. A refused payment returns
// immediately (49.2): what it needed is recorded for the owner to review.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	// r.Context(): a client that walks away frees the handler (and its MFA wait).
	ctx := r.Context()
	var body struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Body    []byte            `json:"body"`
		Headers map[string]string `json:"headers"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err)
		return
	}
	method := strings.ToUpper(body.Method)
	if method == "" {
		method = "GET"
	}
	result, err := s.Gateway.Fetch(ctx, method, body.URL, body.Body, body.Headers)
	if err != nil {
		s.recordIfBlocked(method, body.URL, body.Body, body.Headers, err)
		s.mapAndReply(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleFetchOverride(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		Method              string            `json:"method"`
		URL                 string            `json:"url"`
		Body                []byte            `json:"body"`
		Headers             map[string]string `json:"headers"`
		OverrideAmountMicro int64             `json:"override_amount_micro"`
		ApproveSeller       bool              `json:"approve_seller"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err)
		return
	}
	// This endpoint exists to raise spending authority: either an approved
	// amount or landing a new seller. A request with neither is malformed and
	// answers 400 before the sudo gate — asking for a code and only then
	// reporting "override amount must be positive" wastes the user's window
	// (and used to surface as a 502 from the gateway).
	// amount == 0 with approve_seller: true stays valid (a seller can be landed
	// without a charge).
	if body.OverrideAmountMicro < 0 || (body.OverrideAmountMicro == 0 && !body.ApproveSeller) {
		writeErr(w, http.StatusBadRequest, "bad_request",
			fmt.Errorf("override_amount_micro must be positive (or approve_seller: true)"))
		return
	}
	// Both fields raise spending authority beyond the daily cap (an approved
	// override amount, or landing a new seller), so they need a fresh,
	// CDP-attested MFA verification from the owner.
	if body.OverrideAmountMicro > 0 || body.ApproveSeller {
		if !s.requireSudoMFA(w) {
			return
		}
	}
	method := strings.ToUpper(body.Method)
	if method == "" {
		method = "GET"
	}
	result, err := s.Gateway.FetchWithOverride(ctx, method, body.URL, body.Body, body.Headers, body.OverrideAmountMicro, body.ApproveSeller)
	if err != nil {
		s.recordIfBlocked(method, body.URL, body.Body, body.Headers, err)
		s.mapAndReply(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
