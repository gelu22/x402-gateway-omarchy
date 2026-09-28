// Fetch handlers: /fetch and /fetch-override, including the MFA wait (30.2b).
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"gateway/internal/gateway"
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
	key := "fetch " + method + " " + body.URL
	result, err := s.fetchWithMFAWait(ctx, key, func(ctx context.Context) (*gateway.FetchResult, error) {
		return s.Gateway.Fetch(ctx, method, body.URL, body.Body, body.Headers)
	})
	if err != nil {
		s.mapAndReply(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// fetchWithMFAWait runs fn and, when CDP asks for a code at signing time, waits
// for the verification and completes the payment once (30.2b). The retry runs
// in the background (see mfaWait) so identical requests share one payment and a
// request whose client already left still pays into the cache.
func (s *Server) fetchWithMFAWait(ctx context.Context, key string, fn func(context.Context) (*gateway.FetchResult, error)) (*gateway.FetchResult, error) {
	w := s.mfaWait()
	res, err := fn(ctx)
	if err == nil {
		return res, nil
	}
	switch mapError(err) {
	case "mfa_required":
		// Someone already completed (and cached) this exact request.
		if cached, ok := w.cached(key); ok {
			return cached, nil
		}
		e := w.register(key, fn)
		if !w.await(ctx, e) {
			return nil, err // nothing was paid: keep fail-closed mfa_required
		}
		return e.res, e.err
	case "duplicate_payment":
		// Another flow holds this key: it may be the background retry of the
		// very payment the user just verified — join it before giving up.
		if e, ok := w.lookup(key); ok {
			if w.await(ctx, e) {
				return e.res, e.err
			}
		}
		// The payment is already done; hand back the content it bought.
		if cached, ok := w.cached(key); ok {
			return cached, nil
		}
		return nil, err
	default:
		return nil, err
	}
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
	key := "override " + method + " " + body.URL
	result, err := s.fetchWithMFAWait(ctx, key, func(ctx context.Context) (*gateway.FetchResult, error) {
		return s.Gateway.FetchWithOverride(ctx, method, body.URL, body.Body, body.Headers, body.OverrideAmountMicro, body.ApproveSeller)
	})
	if err != nil {
		s.mapAndReply(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
