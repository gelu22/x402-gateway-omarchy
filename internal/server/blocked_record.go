// blocked_record.go — recordIfBlocked / recordBlocked (split from blocked_store.go for LOC).
package server

import (
	"context"
	"errors"

	"gateway/internal/agentlabel"
	"gateway/internal/gateway"
)

// recordIfBlocked stores a refused payment the owner can act on: a CDP MFA
// request, or an overridable policy denial (budget, per-seller, unknown seller,
// price change). Other errors are not "needs the owner" and are not listed.
func (s *Server) recordIfBlocked(ctx context.Context, method, rawURL string, body []byte, headers map[string]string, err error) {
	var perr *gateway.PolicyError
	if !errors.As(err, &perr) {
		return
	}
	if perr.Code != "mfa_required" && !perr.CanOverride {
		return
	}
	s.recordBlocked(method, rawURL, body, headers, perr.AmountMicro, perr.Code, agentlabel.From(ctx))
}

// recordBlocked is the handler-side entry point: store the refused request and
// notify the owner when the coalescing window allows it.
func (s *Server) recordBlocked(method, rawURL string, body []byte, headers map[string]string, amountMicro int64, reason, agent string) {
	if s.blocked == nil {
		return
	}
	_, notify := s.blocked.Record(method, rawURL, body, headers, amountMicro, reason, agent)
	if notify {
		gateway.Notify("x402 Gateway — payment needs you", "An agent payment was held: "+reason+". Open the panel to review.")
	}
}
