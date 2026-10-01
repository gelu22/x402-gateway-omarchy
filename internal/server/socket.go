// Server transport: HTTP-over-unix socket (CONTRACTS §1).
// Split: socket.go (Serve, ConnContext), handlers.go (route handlers).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gateway/internal/gateway"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code string, err error) {
	msg := err.Error()
	// Canonical envelope (018.1): error/detail only. Deprecated code/message
	// cut in 018.5 — panel reads via Model.errorCode/errorDetail.
	writeJSON(w, status, map[string]string{"error": code, "detail": msg})
}

// mapError maps a gateway error to the CONTRACTS §1 wire code. Policy denials
// keep their specific code (budget_exceeded, price_changed, unknown_seller, …)
// and sentinels map to their documented names — collapsing them to a generic
// "policy_error"/"server_error" hides the reason agents branch on.
// Mirrors gateway.auditErrorCode; keep the two vocabularies in sync.
func mapError(err error) string {
	if err == nil {
		return "ok"
	}
	var perr *gateway.PolicyError
	if errors.As(err, &perr) {
		if perr.Code != "" {
			return perr.Code
		}
		return "policy_error" // defensive: malformed PolicyError
	}
	switch {
	case errors.Is(err, gateway.ErrPaused):
		return "paused"
	case errors.Is(err, gateway.ErrDuplicate):
		return "duplicate_payment"
	case errors.Is(err, gateway.ErrBadTarget):
		return "bad_target"
	case errors.Is(err, gateway.ErrSigner):
		return "signer_error"
	case errors.Is(err, gateway.ErrNoRequirements):
		return "no_requirements"
	case errors.Is(err, gateway.ErrUpstream):
		return "upstream_error"
	case errors.Is(err, gateway.ErrContentTooLarge):
		return "content_too_large"
	default:
		return "server_error"
	}
}

// logPeer records the SO_PEERCRED of a unix client.
func logPeer(logger *slog.Logger, c net.Conn) {
	uid, pid, ok := peerCred(c)
	if !ok {
		logger.Debug("socket peer unknown")
		return
	}
	logger.Info("socket peer", "uid", uid, "pid", pid)
}

const maxBodyBytes = 1 << 20

type Server struct {
	Gateway     *gateway.Gateway
	Pairing     PairingAPI
	MFA         MFAAPI
	Version     string
	logger      *slog.Logger
	AuditLogger *slog.Logger

	// MFA wait registry + result cache (30.2b), lazily created by mfaWait().
	mfaMu sync.Mutex
	mfa   *mfaWait

	// sudoConsumed is the CDP verification stamp already spent on a spending
	// authority raise (46.7). A verification is good for exactly one raise;
	// without this, one code entry authorised N raises inside mfaSudoWindow.
	sudoMu       sync.Mutex
	sudoConsumed string
}

type PairingAPI interface {
	InitPairing(ctx context.Context, email string) (flowID, message string, err error)
	VerifyPairing(ctx context.Context, flowID, otp string) error
	PairState() (state, email, walletAddress string)
	Logout() error
}

func Serve(socketPath, version string, gw *gateway.Gateway, pairing PairingAPI, mfa MFAAPI, logger, auditLogger *slog.Logger) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return fmt.Errorf("server: state dir: %w", err)
	}
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("server: listen: %w", err)
	}
	_ = os.Chmod(socketPath, 0o600)
	srv := &Server{Gateway: gw, Pairing: pairing, MFA: mfa, Version: version, logger: logger, AuditLogger: auditLogger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", srv.handleStatus)
	mux.HandleFunc("GET /pause", srv.handlePause)
	mux.HandleFunc("POST /pause", srv.handlePause)
	mux.HandleFunc("GET /pair/status", srv.handlePairStatus)
	mux.HandleFunc("POST /pair/init", srv.handlePairInit)
	mux.HandleFunc("POST /pair/verify", srv.handlePairVerify)
	mux.HandleFunc("POST /pair/logout", srv.handlePairLogout)
	mux.HandleFunc("POST /fetch", srv.handleFetch)
	mux.HandleFunc("POST /fetch-override", srv.handleFetchOverride)
	mux.HandleFunc("GET /policy", srv.handlePolicy)
	mux.HandleFunc("POST /policy", srv.handlePolicy)
	mux.HandleFunc("/mfa/enroll/init", srv.handleMfaEnrollInit)
	mux.HandleFunc("/mfa/enroll/submit", srv.handleMfaEnrollSubmit)
	mux.HandleFunc("/mfa/verify/init", srv.handleMfaVerifyInit)
	mux.HandleFunc("/mfa/verify/submit", srv.handleMfaVerifySubmit)
	logger.Info("socket listening", "path", socketPath)
	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// One endpoint (/fetch, /fetch-override) legitimately waits for a human
		// to type an MFA code (30.2b); the grace keeps the wait from being cut
		// off. Local unix socket, trusted same-user client, few connections.
		WriteTimeout:   mfaWaitTimeout + mfaWaitGrace,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 16 * 1024,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			logPeer(logger, c)
			return ctx
		},
	}
	return httpSrv.Serve(ln)
}
