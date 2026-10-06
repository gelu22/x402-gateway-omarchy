package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"gateway/internal/agentlabel"
	"gateway/internal/policy"
)

func agentCapPolicy(t *testing.T, gw *Gateway, daily, agentDefault int64, overrides map[string]int64) {
	t.Helper()
	p := policy.Default()
	p.DailyCapMicro = daily
	p.DomainSubCapPercent = 0 // httptest shares one host
	p.AgentDailyCapMicro = agentDefault
	p.AgentCapsMicro = overrides
	gw.SetPolicy(p)
}

func fetchAs(t *testing.T, gw *Gateway, agent, url string) error {
	t.Helper()
	ctx := context.Background()
	if agent != "" {
		ctx = agentlabel.With(ctx, agent)
	}
	_, err := gw.Fetch(ctx, http.MethodGet, url, nil, nil)
	return err
}

// uniqueURL returns a distinct path under the same seller (avoids duplicate_payment).
func uniqueURL(base string, n int) string {
	return base + "?n=" + strconv.Itoa(n)
}

func TestAgentCapThirdPaymentDenied(t *testing.T) {
	gw, _ := newSettleGateway(t)
	agentCapPolicy(t, gw, 50_000_000, 2_000_000, nil) // 2 USDC agent default
	var buf bytes.Buffer
	gw.Logger = slog.New(slog.NewJSONHandler(&buf, nil))
	srv := sellerAsking(t, "1000000", http.StatusOK) // 1 USDC
	base := srv.URL + "/content"
	signer := gw.Signer.(*settleSigner)

	if err := fetchAs(t, gw, "codex", uniqueURL(base, 1)); err != nil {
		t.Fatalf("pay 1: %v", err)
	}
	if err := fetchAs(t, gw, "codex", uniqueURL(base, 2)); err != nil {
		t.Fatalf("pay 2: %v", err)
	}
	signsBefore := signer.signCalls.Load()
	err := fetchAs(t, gw, "codex", uniqueURL(base, 3))
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "agent_cap_exceeded" {
		t.Fatalf("pay 3: want agent_cap_exceeded, got %v", err)
	}
	if !perr.CanOverride {
		t.Fatal("agent_cap_exceeded must be overridable")
	}
	if signer.signCalls.Load() != signsBefore {
		t.Fatal("agent_cap_exceeded must not call Sign")
	}
	// Audit line carries agent + outcome.
	var found bool
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if m["msg"] == "payment audit" && m["outcome"] == "failed:agent_cap_exceeded" {
			if m["agent"] != "codex" {
				t.Fatalf("audit agent = %v, want codex", m["agent"])
			}
			found = true
		}
	}
	if !found {
		t.Fatal("want audit failed:agent_cap_exceeded with agent=codex")
	}
}

func TestAgentCapOtherLabelPays(t *testing.T) {
	gw, _ := newSettleGateway(t)
	agentCapPolicy(t, gw, 50_000_000, 2_000_000, nil)
	srv := sellerAsking(t, "1000000", http.StatusOK)
	base := srv.URL + "/content"
	if err := fetchAs(t, gw, "codex", uniqueURL(base, 1)); err != nil {
		t.Fatal(err)
	}
	if err := fetchAs(t, gw, "codex", uniqueURL(base, 2)); err != nil {
		t.Fatal(err)
	}
	if err := fetchAs(t, gw, "claude", uniqueURL(base, 3)); err != nil {
		t.Fatalf("other label must pay: %v", err)
	}
}

func TestAgentCapEmptyLabelUsesDefaultBucket(t *testing.T) {
	gw, _ := newSettleGateway(t)
	agentCapPolicy(t, gw, 50_000_000, 2_000_000, nil)
	srv := sellerAsking(t, "1000000", http.StatusOK)
	base := srv.URL + "/content"
	if err := fetchAs(t, gw, "", uniqueURL(base, 1)); err != nil {
		t.Fatal(err)
	}
	if err := fetchAs(t, gw, "", uniqueURL(base, 2)); err != nil {
		t.Fatal(err)
	}
	err := fetchAs(t, gw, "", uniqueURL(base, 3))
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "agent_cap_exceeded" {
		t.Fatalf("empty label shares default bucket: want agent_cap_exceeded, got %v", err)
	}
}

func TestAgentCapOverridePays(t *testing.T) {
	gw, _ := newSettleGateway(t)
	agentCapPolicy(t, gw, 50_000_000, 2_000_000, nil)
	srv := sellerAsking(t, "1000000", http.StatusOK)
	base := srv.URL + "/content"
	ctx := agentlabel.With(context.Background(), "codex")
	if _, err := gw.Fetch(ctx, http.MethodGet, uniqueURL(base, 1), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Fetch(ctx, http.MethodGet, uniqueURL(base, 2), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.FetchWithOverride(ctx, http.MethodGet, uniqueURL(base, 3), nil, nil, 1_000_000, false); err != nil {
		t.Fatalf("owner override must lift agent cap: %v", err)
	}
}

func TestAgentCapPermissionDoesNotLift(t *testing.T) {
	gw, _ := newSettleGateway(t)
	agentCapPolicy(t, gw, 50_000_000, 2_000_000, nil)
	gw.Sellers = NewSellerRegistry(t.TempDir())
	gw.Permissions = NewPermissionStore(t.TempDir(), nil)
	srv := sellerAsking(t, "1000000", http.StatusOK)
	base := srv.URL + "/content"
	ctx := agentlabel.With(context.Background(), "codex")
	for i := 1; i <= 2; i++ {
		u := uniqueURL(base, i)
		if err := gw.Permissions.Add(u, 1_000_000, false, 0); err != nil {
			t.Fatal(err)
		}
		if _, err := gw.Fetch(ctx, http.MethodGet, u, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	u3 := uniqueURL(base, 3)
	if err := gw.Permissions.Add(u3, 1_000_000, false, 0); err != nil {
		t.Fatal(err)
	}
	_, err := gw.Fetch(ctx, http.MethodGet, u3, nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "agent_cap_exceeded" {
		t.Fatalf("permission must not lift agent cap, got %v", err)
	}
}

func TestAgentCapDailyTakesPriority(t *testing.T) {
	gw, _ := newSettleGateway(t)
	// Daily cap = 1 USDC; agent cap = 1 USDC. After one payment both are spent;
	// next denial must be budget_exceeded (checked first).
	agentCapPolicy(t, gw, 1_000_000, 1_000_000, nil)
	srv := sellerAsking(t, "1000000", http.StatusOK)
	base := srv.URL + "/content"
	ctx := agentlabel.With(context.Background(), "codex")
	if _, err := gw.Fetch(ctx, http.MethodGet, uniqueURL(base, 1), nil, nil); err != nil {
		t.Fatal(err)
	}
	_, err := gw.Fetch(ctx, http.MethodGet, uniqueURL(base, 2), nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "budget_exceeded" {
		t.Fatalf("want budget_exceeded when both caps spent, got %v", err)
	}
}
