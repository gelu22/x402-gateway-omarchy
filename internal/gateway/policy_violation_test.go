package gateway

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 30.3: a CDP Policy Engine rejection is a decision, not a fault. The daemon
// must surface it as its own non-overridable code and settle nothing — an agent
// reading signer_error would retry a wall.
func TestPolicyViolationIsSurfacedAndSettlesNothing(t *testing.T) {
	gw, payments := newSettleGateway(t)

	fakeSign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errorType":"policy_violation","errorMessage":"policy rejected the signature"}`))
	}))
	t.Cleanup(fakeSign.Close)
	gw.Client.BaseURL = fakeSign.URL

	var buf bytes.Buffer
	gw.Logger = slog.New(slog.NewJSONHandler(&buf, nil))

	seller := sellerWith(t, http.StatusOK)
	_, err := gw.Fetch(context.Background(), http.MethodGet, seller.URL+"/content", nil, nil)

	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "policy_violation" {
		t.Fatalf("want policy_violation, got %v", err)
	}
	if perr.CanOverride {
		t.Fatal("a TEE policy ceiling must not be budget-overridable")
	}
	if payments.Load() != 0 {
		t.Fatal("nothing may settle when the policy engine refuses")
	}
	if s, _ := gw.Spend.Today(); s != 0 {
		t.Fatalf("spend = %d, want 0", s)
	}
	if gw.lastFetchError == nil || gw.lastFetchError.Code != "policy_violation" {
		t.Fatalf("last_fetch_error must carry policy_violation, got %+v", gw.lastFetchError)
	}
	if !gw.lastFetchError.CanOverride == false {
		t.Fatal("last_fetch_error must not offer an override")
	}
	// 37.1/F4: one attempt = exactly one audit line with the real amount.
	lines, _ := decodeAuditLines(t, buf.String())
	if len(lines) != 1 {
		t.Fatalf("audit lines = %d, want exactly 1 per attempt", len(lines))
	}
	if m := lines[0]; m["outcome"] != "failed:policy_violation" || m["amount_micro"] != float64(10_000) || m["override"] != false {
		t.Fatalf("wrong policy_violation audit line: %v", m)
	}
}
