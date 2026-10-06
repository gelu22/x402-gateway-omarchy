package gateway

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"gateway/internal/agentlabel"
)

// 54.3: every LogPayment call site must emit agent (empty or set). Table covers
// the seven production outcomes; agent travels via ctx into doFetch where exercised.
func TestPaymentAuditAgentOnSevenPaths(t *testing.T) {
	type path struct {
		name    string
		run     func(t *testing.T, gw *Gateway, agent string) error
		wantOut string
	}
	secretURL := "https://seller.example/paid?token=S3CR3T"
	paths := []path{
		{
			name: "defer-failure-paused",
			run: func(t *testing.T, gw *Gateway, agent string) error {
				gw.Paused.Store(true)
				ctx := context.Background()
				if agent != "" {
					ctx = agentlabel.With(ctx, agent)
				}
				_, err := gw.Fetch(ctx, http.MethodGet, secretURL, nil, nil)
				return err
			},
			wantOut: "failed:paused",
		},
		{
			name: "policy-deny-unknown_seller",
			run: func(t *testing.T, gw *Gateway, agent string) error {
				gw.Sellers = NewSellerRegistry(t.TempDir())
				url := sellerWith(t, http.StatusOK).URL + "/content"
				ctx := context.Background()
				if agent != "" {
					ctx = agentlabel.With(ctx, agent)
				}
				_, err := gw.Fetch(ctx, http.MethodGet, url, nil, nil)
				return err
			},
			wantOut: "failed:unknown_seller",
		},
		{
			name: "network_denied",
			run: func(t *testing.T, gw *Gateway, agent string) error {
				LogPayment(gw.Logger, PaymentLine{
					Target: secretURL, Outcome: policyOutcome("network_denied"), Agent: agent,
				})
				return nil
			},
			wantOut: "failed:network_denied",
		},
		{
			name: "insufficient_funds",
			run: func(t *testing.T, gw *Gateway, agent string) error {
				LogPayment(gw.Logger, PaymentLine{
					AmountMicro: 5000, Target: secretURL,
					Outcome: policyOutcome("insufficient_funds"), Agent: agent,
				})
				return nil
			},
			wantOut: "failed:insufficient_funds",
		},
		{
			name: "domain_cap",
			run: func(t *testing.T, gw *Gateway, agent string) error {
				LogPayment(gw.Logger, PaymentLine{
					AmountMicro: 1000, Target: secretURL,
					Outcome: "failed:domain_cap_exceeded", Agent: agent,
				})
				return nil
			},
			wantOut: "failed:domain_cap_exceeded",
		},
		{
			name: "budget",
			run: func(t *testing.T, gw *Gateway, agent string) error {
				LogPayment(gw.Logger, PaymentLine{
					AmountMicro: 1000, Target: secretURL,
					Outcome: "failed:budget_exceeded", Agent: agent,
				})
				return nil
			},
			wantOut: "failed:budget_exceeded",
		},
		{
			name: "paid",
			run: func(t *testing.T, gw *Gateway, agent string) error {
				LogPayment(gw.Logger, PaymentLine{
					AmountMicro: 1000, Target: secretURL,
					Outcome: "paid", Agent: agent,
				})
				return nil
			},
			wantOut: "paid",
		},
	}

	for _, agent := range []string{"opencode", ""} {
		for _, p := range paths {
			t.Run(p.name+"/agent="+agent, func(t *testing.T) {
				gw, _ := newSettleGateway(t)
				var buf bytes.Buffer
				gw.Logger = slog.New(slog.NewJSONHandler(&buf, nil))
				_ = p.run(t, gw, agent)
				lines, raw := decodeAuditLines(t, buf.String())
				if len(lines) < 1 {
					t.Fatalf("want ≥1 audit line")
				}
				m := lines[len(lines)-1]
				assertAuditKeys(t, m)
				if m["outcome"] != p.wantOut {
					t.Fatalf("outcome = %v, want %s", m["outcome"], p.wantOut)
				}
				got, _ := m["agent"].(string)
				if got != agent {
					t.Fatalf("agent = %q, want %q", got, agent)
				}
				if strings.Contains(raw, "S3CR3T") || strings.Contains(raw, "/paid") {
					t.Fatalf("URL leaked: %s", raw)
				}
			})
		}
	}
}

func TestLogPaymentAlwaysHasAgentKey(t *testing.T) {
	lines, _ := collectAudit(t, func(l *slog.Logger) {
		LogPayment(l, PaymentLine{AmountMicro: 1, Target: "https://a.example/", Outcome: "paid"})
	})
	if _, ok := lines[0]["agent"]; !ok {
		t.Fatal("missing agent key")
	}
	if lines[0]["agent"] != "" {
		t.Fatalf("want empty agent, got %v", lines[0]["agent"])
	}
}
