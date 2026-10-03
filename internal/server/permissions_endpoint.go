// /permissions (49.3): the owner's pre-approvals. GET is read-only; POST and
// DELETE raise or lower spending authority, so they go through the sudo gate
// (a fresh CDP MFA code), like every other authority raise.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gateway/internal/gateway"
)

func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"permissions": s.permissions().List()})
	case http.MethodPost:
		s.handlePermissionAdd(w, r)
	case http.MethodDelete:
		s.handlePermissionRemove(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "bad_request", fmt.Errorf("method %s not allowed", r.Method))
	}
}

func (s *Server) handlePermissionAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL        string `json:"url"`
		LimitMicro int64  `json:"limit_micro"`
		Temporary  bool   `json:"temporary"`
		TTLSeconds int64  `json:"ttl_seconds"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err)
		return
	}
	// Raising spending authority: fresh MFA before anything is stored.
	if !s.requireSudoMFA(w) {
		return
	}
	var ttl time.Duration
	if body.Temporary {
		if body.TTLSeconds <= 0 {
			writeErr(w, http.StatusBadRequest, "bad_request", fmt.Errorf("temporary requires ttl_seconds > 0"))
			return
		}
		ttl = time.Duration(body.TTLSeconds) * time.Second
	}
	if err := s.permissions().Add(body.URL, body.LimitMicro, body.Temporary, ttl); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": s.permissions().List()})
}

func (s *Server) handlePermissionRemove(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		writeErr(w, http.StatusBadRequest, "bad_request", fmt.Errorf("url query parameter required"))
		return
	}
	if !s.requireSudoMFA(w) {
		return
	}
	if !s.permissions().Remove(rawURL) {
		writeErr(w, http.StatusNotFound, "not_found", fmt.Errorf("no permission for that url"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": s.permissions().List()})
}

// permissions returns the gateway's permission store, or nil (deny-all) when the
// gateway is not wired — the store's methods are nil-safe.
func (s *Server) permissions() *gateway.PermissionStore {
	if s.Gateway == nil {
		return nil
	}
	return s.Gateway.Permissions
}
