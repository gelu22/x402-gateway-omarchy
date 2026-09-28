package gateway

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestMfaRequiredBlocksAndRetriesAfterVerify proves the fail-closed MFA path:
// a sign rejected with mfa_required settles nothing and surfaces explicitly;
// a fresh fetch after out-of-band verification pays normally.
func TestMfaRequiredBlocksAndRetriesAfterVerify(t *testing.T) {
	gw, payments := newSettleGateway(t)

	requireMFA := atomic.Bool{}
	requireMFA.Store(true)
	fakeSign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if requireMFA.Load() {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errorType":"mfa_required","errorMessage":"MFA required"}`))
			return
		}
		_, _ = w.Write([]byte(`{"signature":"0x` + strings.Repeat("a", 130) + `"}`))
	}))
	t.Cleanup(fakeSign.Close)
	gw.Client.BaseURL = fakeSign.URL

	seller := sellerWith(t, http.StatusOK)
	url := seller.URL + "/content"

	var buf bytes.Buffer
	gw.Logger = slog.New(slog.NewJSONHandler(&buf, nil))

	// 1. Signing blocked: explicit mfa_required, nothing settles, no spend.
	_, err := gw.Fetch(context.Background(), http.MethodGet, url, nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "mfa_required" {
		t.Fatalf("want mfa_required, got %v", err)
	}
	if perr.CanOverride {
		t.Fatal("mfa_required must not be budget-overridable")
	}
	if payments.Load() != 0 {
		t.Fatal("no payment must settle while MFA is missing")
	}
	if s, _ := gw.Spend.Today(); s != 0 {
		t.Fatalf("spend = %d, want 0", s)
	}
	if gw.lastFetchError == nil || gw.lastFetchError.Code != "mfa_required" {
		t.Fatal("last_fetch_error must carry mfa_required for the panel")
	}
	// 37.1/F4: one attempt = exactly one audit line (the doFetch defer), with
	// the real amount — the explicit log in fetch_sign used to duplicate it.
	lines, _ := decodeAuditLines(t, buf.String())
	if len(lines) != 1 {
		t.Fatalf("audit lines = %d, want exactly 1 per attempt", len(lines))
	}
	if m := lines[0]; m["outcome"] != "failed:mfa_required" || m["amount_micro"] != float64(10_000) || m["override"] != false {
		t.Fatalf("wrong mfa_required audit line: %v", m)
	}

	// 2. Out-of-band verification (MFA session now exists) → fresh retry pays.
	// No dedup block: the failed attempt never reached markSigned.
	requireMFA.Store(false)
	res, err := gw.Fetch(context.Background(), http.MethodGet, url, nil, nil)
	if err != nil {
		t.Fatalf("retry after verify: %v", err)
	}
	if res.Status != http.StatusOK || payments.Load() != 1 {
		t.Fatalf("want settled retry, got status=%d payments=%d", res.Status, payments.Load())
	}
	// A successful payment still logs exactly once (the explicit "paid" line);
	// the defer stays silent on err == nil.
	lines, _ = decodeAuditLines(t, buf.String())
	if len(lines) != 2 { // 1 denial + 1 paid
		t.Fatalf("audit lines = %d, want 2 (denial + paid)", len(lines))
	}
	if last := lines[len(lines)-1]; last["outcome"] != "paid" || last["amount_micro"] != float64(10_000) {
		t.Fatalf("wrong paid audit line: %v", last)
	}
}
