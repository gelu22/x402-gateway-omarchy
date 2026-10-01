// buildGateway wires the session Manager (Signer), policy and spend tracker.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gateway/internal/budget"
	"gateway/internal/cdp"
	"gateway/internal/config"
	"gateway/internal/gateway"
	"gateway/internal/policy"
	"gateway/internal/session"
	"gateway/internal/spend"
	"gateway/internal/telemetry"
)

func buildGateway(cfg *config.Config, logger *slog.Logger) (*gateway.Gateway, *session.Manager, *telemetry.Client, error) {
	pol, err := policy.Load(cfg.StateDir)
	if err != nil {
		return nil, nil, nil, err
	}
	gw := &gateway.Gateway{
		Client:       cdp.NewClient(cfg.ProjectID),
		PolicyPath:   filepath.Join(cfg.StateDir, "policy.json"),
		AllowPrivate: os.Getenv("GATEWAY_ALLOW_PRIVATE") == "1",
		Spend:        spend.NewTracker(cfg.StateDir),
		Budget:       budget.NewAuthority(cfg.StateDir, nil),
		Logger:       logger,
		Sellers:      gateway.NewSellerRegistry(cfg.StateDir),
	}
	ssrf := &gateway.SsrfGuard{}
	ssrf.SetAllowPrivate(gw.AllowPrivate)
	gw.Ssrf = ssrf

	httpClient := &http.Client{
		Timeout: gateway.FetchTimeout,
		Transport: &http.Transport{
			DialContext: gateway.DialContext(ssrf, &net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}),
		},
	}
	gw.HTTP = httpClient

	mgr, err := session.New(gw.Client, session.NewStore(cfg.StateDir), logger)
	if err != nil {
		return nil, nil, nil, err
	}
	gw.Blocks = gateway.NewBlockTracker(cfg.StateDir, nil)
	gw.Balance = gateway.NewBalanceFetcher(cfg.Network)
	gw.PaymentNetwork = cfg.Network

	tel := telemetry.New(os.Getenv("GATEWAY_BACKEND_URL"), cfg.StateDir)
	tel.LoadQueue()
	gw.SetPolicy(pol)
	gw.Signer = mgr
	gw.SessionState = mgr.State
	gw.MFAState = func() (bool, string) {
		ctx := context.Background()
		return mgr.MfaStatus(ctx)
	}
	if cfg.CDPBaseURL != "" {
		gw.Client.BaseURL = cfg.CDPBaseURL
	}
	// OnPayment: 2xx-only telemetry + balance cache. Domain spend is charged
	// in fetch_sign.chargeDomain on every post-sig Commit (44.repass.1).
	gw.OnPayment = func(amountMicro int64, domain string) {
		if gw.Balance != nil {
			gw.Balance.Invalidate()
		}
		if tel != nil {
			tel.Enqueue(telemetry.Event{
				Type:        "payment",
				Domain:      domain,
				AmountMicro: amountMicro,
			})
		}
	}
	logger.Info("session resumed", "state", mgr.State())
	return gw, mgr, tel, nil
}
