// GET /history — newest payment-audit rows for the panel (54.7).
package server

import (
	"fmt"
	"net/http"
	"strconv"

	"gateway/internal/gateway"
)

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", fmt.Errorf("GET only"))
		return
	}
	limit := 50
	if q := r.URL.Query().Get("limit"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "bad_request", fmt.Errorf("limit must be 1..200"))
			return
		}
		if n > 200 {
			n = 200
		}
		limit = n
	}
	entries, truncated, err := gateway.ReadPaymentHistory(s.AuditPath, limit, gateway.HistoryMaxBytes)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "history_unavailable", fmt.Errorf("audit read failed"))
		return
	}
	if entries == nil {
		entries = []gateway.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries":   entries,
		"truncated": truncated,
	})
}
