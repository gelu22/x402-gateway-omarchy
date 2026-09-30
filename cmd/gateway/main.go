// Command gateway: main(), signal handling, singleton, audit, serve.
// buildGateway() is in build.go.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"gateway/internal/config"
	"gateway/internal/gateway"
	"gateway/internal/mcpserver"
	"gateway/internal/server"
)

// version is the build version; overridden at release time via
// -ldflags "-X main.version=<tag>" (see .github/workflows/release.yml).
// Local builds keep the "dev" fallback.
var version = "dev"

func main() {
	// Lifecycle subcommands (42.3): must run before flag.Parse so --help of
	// the daemon does not swallow `install --bundle`.
	if len(os.Args) > 1 && (os.Args[1] == "install" || os.Args[1] == "self-remove") {
		os.Exit(runInstallCmd(os.Args[1:]))
	}

	mcpMode := flag.Bool("mcp", false, "run as MCP stdio server (for AI agent configs)")
	socketPath := flag.String("socket-path", "", "unix socket path (default $XDG_STATE_DIR/x402-gateway/gw.sock)")
	stateDir := flag.String("state-dir", "", "state directory (default $XDG_STATE_DIR/x402-gateway)")
	showVersion := flag.Bool("version", false, "print version and exit")
	showStatus := flag.Bool("status", false, "print a read-only diagnostic snapshot and exit (creates empty state dir if missing, otherwise never mutates)")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if *mcpMode {
		cfg, err := config.Load(version, *socketPath, *stateDir)
		if err != nil {
			logger.Error("config", "err", err)
			os.Exit(1)
		}
		if err := mcpserver.RunStdio(context.Background(), mcpserver.Options{
			SocketPath: cfg.SocketPath,
			Version:    cfg.Version,
		}); err != nil {
			logger.Error("mcp", "err", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(version, *socketPath, *stateDir)
	if err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}
	if *showStatus {
		os.Exit(runStatus(cfg))
	}
	if cfg.ProjectID == "" {
		logger.Warn("auth disabled: CDP_PROJECT_ID not set; /fetch will fail, /status works")
	}
	if os.Getenv("GATEWAY_ALLOW_PRIVATE") == "1" {
		logger.Warn("SSRF guard DISABLED: GATEWAY_ALLOW_PRIVATE=1 — dev/test only, never in production installs")
	}

	// Hardening runs only for the long-running daemon: --status/--version have
	// already exited and -mcp is a thin stdio bridge without session or TWS.
	hardenProcess(logger)

	logger.Info("gateway starting",
		"version", cfg.Version,
		"socket", cfg.SocketPath,
		"state_dir", cfg.StateDir,
		"network", cfg.Network,
	)

	gw, mgr, tel, err := buildGateway(cfg, logger)
	if err != nil {
		logger.Error("gateway init", "err", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mgr.Run(ctx)
	go func() {
		for {
			if _, err := tel.Flush(""); err != nil {
				logger.Warn("telemetry flush", "err", err.Error())
			}
			time.Sleep(5 * time.Minute)
		}
	}()
	mgr.OnSignedIn = func(accessToken string) {
		if err := tel.Register(accessToken); err != nil {
			logger.Warn("telemetry register", "err", err.Error())
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		fmt.Fprintln(os.Stderr, "shutdown requested")
		// Wipe the signing secret before Persist/exit: os.Exit skips defers,
		// so cleanup has to be explicit here.
		mgr.DropTWS()
		if err := mgr.Persist(); err != nil {
			logger.Warn("session persist on shutdown", "err", err)
		}
		_ = os.Remove(cfg.SocketPath) // best-effort stale-socket cleanup
		os.Exit(0)
	}()

	// Single-instance guard: a duplicate daemon would race refresh-token
	// rotation and get the session revoked. Exit 0 (not 1) so Service.qml
	// does not restart-loop a duplicate.
	releaseLock, err := server.AcquireSingleton(cfg.SocketPath + ".lock")
	if err != nil {
		logger.Error("gateway already running", "err", err)
		os.Exit(0)
	}
	defer releaseLock()

	// Money audit sink (011.1): append-only JSONL, durable and shell-independent.
	auditPath := filepath.Join(cfg.StateDir, "audit.log")
	if err := gateway.RotateAuditLog(auditPath, gateway.MaxAuditBytes); err != nil {
		logger.Error("audit rotate", "err", err)
	}
	auditFile, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304
	if err != nil {
		logger.Error("audit open", "path", auditPath, "err", err)
		os.Exit(1)
	}
	auditLogger := slog.New(slog.NewJSONHandler(auditFile, nil))
	gw.Logger = auditLogger

	if err := server.Serve(cfg.SocketPath, cfg.Version, gw, mgr, mgr, logger, auditLogger); err != nil {
		logger.Error("serve", "err", err)
		os.Exit(1)
	}
}
