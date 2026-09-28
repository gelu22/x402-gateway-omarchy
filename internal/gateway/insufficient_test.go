package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeBalanceRPC serves one eth_call response (USDC balanceOf) with result body.
func fakeBalanceRPC(t *testing.T, result string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + result + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func fetcherAt(t *testing.T, url string) *BalanceFetcher {
	t.Helper()
	bf := NewBalanceFetcher("eip155:84532")
	bf.RPCURL = url
	return bf
}

// 33.2: a KNOWN balance below the amount blocks before signing — nothing settles
// and the agent gets a non-overridable 402 code instead of an upstream error.
func TestInsufficientFundsBlocksBeforeSigning(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Balance = fetcherAt(t, fakeBalanceRPC(t, "0x0")) // 0 USDC

	_, err := gw.Fetch(context.Background(), http.MethodGet, sellerWith(t, http.StatusOK).URL+"/content", nil, nil)

	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "insufficient_funds" {
		t.Fatalf("want PolicyError insufficient_funds, got %v", err)
	}
	if perr.CanOverride {
		t.Fatal("insufficient_funds must not be overridable")
	}
	if payments.Load() != 0 {
		t.Fatalf("payments = %d, want 0 (no settle without funds)", payments.Load())
	}
	if gw.lastError() == nil || gw.lastError().Code != "insufficient_funds" {
		t.Fatalf("last_fetch_error must carry insufficient_funds, got %+v", gw.lastError())
	}
}

// An RPC outage must not look like "no funds": the check is advisory and the
// settle path stays the fail-closed gate.
func TestInsufficientFundsAllowsWhenBalanceUnknown(t *testing.T) {
	gw, payments := newSettleGateway(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	gw.Balance = fetcherAt(t, srv.URL)

	res, err := gw.Fetch(context.Background(), http.MethodGet, sellerWith(t, http.StatusOK).URL+"/content", nil, nil)
	if err != nil {
		t.Fatalf("payment must proceed when balance is unknown: %v", err)
	}
	if res == nil || res.Status != http.StatusOK {
		t.Fatalf("want 200 from seller, got %+v", res)
	}
	if payments.Load() != 1 {
		t.Fatalf("payments = %d, want 1", payments.Load())
	}
}
