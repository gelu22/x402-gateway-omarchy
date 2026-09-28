package gateway

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gateway/internal/cdp"
	"gateway/internal/policy"
	"gateway/internal/spend"
)

// settleSigner returns a locally generated WalletSecret so XWalletAuth signs
// offline (ES256 JWT); the CDP HTTP call itself is faked with httptest.
// signCalls counts WalletSecret fetches (i.e. signing attempts), so tests can
// prove an error path never reached signing.
type settleSigner struct {
	ws        *cdp.WalletSecret
	signCalls atomic.Int32
}

func (s *settleSigner) Address() string              { return "0xe6D2863Eb960a980eC3714f85568f974d03cE04E" }
func (s *settleSigner) UserID() string               { return "test-user" }
func (s *settleSigner) AccessToken() (string, error) { return "tok", nil }
func (s *settleSigner) WalletSecret() (*cdp.WalletSecret, error) {
	s.signCalls.Add(1)
	return s.ws, nil
}

// newSettleGateway wires a gateway that can actually reach the signed retry:
// local signer + fake CDP sign endpoint + counted OnPayment.
func newSettleGateway(t *testing.T) (*Gateway, *atomic.Int32) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw := &Gateway{
		Spend:        spend.NewTracker(t.TempDir()),
		Signer:       &settleSigner{ws: cdp.NewWalletSecret("test-secret", time.Time{}, nil, key)},
		AllowPrivate: true,
		Blocks:       NewBlockTracker(t.TempDir(), nil),
	}
	p := policy.Default()
	p.DailyCapMicro = 5_000_000
	gw.SetPolicy(p)

	gw.Client = cdp.NewClient("test-project")
	fakeSign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"signature":"0x` + strings.Repeat("a", 130) + `"}`))
	}))
	t.Cleanup(fakeSign.Close)
	gw.Client.BaseURL = fakeSign.URL

	var payments atomic.Int32
	// Mirror production OnPayment (main.go): the side effects live here, so the
	// test proves they run only on settle, not that Spend works in isolation.
	gw.OnPayment = func(amountMicro int64, domain string) {
		payments.Add(1)
		if err := gw.Spend.Add(amountMicro); err != nil {
			t.Errorf("spend add: %v", err)
		}
	}
	return gw, &payments
}

// sellerWith answers the unsigned hit with valid 402 requirements and the
// signed retry with retryStatus.
func sellerWith(t *testing.T, retryStatus int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Payment-Signature") != "" {
			w.WriteHeader(retryStatus)
			_, _ = w.Write([]byte("x"))
			return
		}
		w.Header().Set("Payment-Required", paymentRequiredHeaderWith("10000", usdcBaseSepolia, "eip155:84532"))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// spendToday reads the single daily spend counter; shared by the suite.
func spendToday(t *testing.T, gw *Gateway) int64 {
	t.Helper()
	spent, err := gw.Spend.Today()
	if err != nil {
		t.Fatal(err)
	}
	return spent
}

// 011.3: a settled (2xx) payment records spend, telemetry and clears the block.
func TestSettleRecordsOn2xx(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Blocks.Record(BlockRecord{Reason: "budget_exceeded"})

	if _, err := gw.Fetch(context.Background(), http.MethodGet, sellerWith(t, http.StatusOK).URL+"/content", nil, nil); err != nil {
		t.Fatalf("want success, got %v", err)
	}
	if got := payments.Load(); got != 1 {
		t.Fatalf("OnPayment calls = %d, want 1", got)
	}
	if spent := spendToday(t, gw); spent != 10_000 {
		t.Fatalf("spend = %d, want 10000", spent)
	}
	if gw.Blocks.Current() != nil {
		t.Fatal("settled payment must clear the block")
	}
}

// 011.3: a payment rejected after signature records nothing and keeps the block.
func TestRejectedAfterSignDoesNotRecord(t *testing.T) {
	gw, payments := newSettleGateway(t)
	gw.Blocks.Record(BlockRecord{Reason: "budget_exceeded"})

	if _, err := gw.Fetch(context.Background(), http.MethodGet, sellerWith(t, http.StatusPaymentRequired).URL+"/content", nil, nil); err == nil {
		t.Fatal("want rejection error")
	}
	if got := payments.Load(); got != 0 {
		t.Fatalf("OnPayment calls = %d, want 0 on rejection", got)
	}
	if spent := spendToday(t, gw); spent != 0 {
		t.Fatalf("spend = %d, want 0 on rejection", spent)
	}
	if gw.Blocks.Current() == nil {
		t.Fatal("block must survive a rejected payment")
	}
}

// 013.1: a post-sign non-2xx is a fail-closed error, never a proxied success.
// (Previously returned (result, nil): an agent saw "success" with a 500 body.)
func TestPostSign5xxFailsClosed(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			gw, payments := newSettleGateway(t)
			gw.Blocks.Record(BlockRecord{Reason: "budget_exceeded"})
			var buf bytes.Buffer
			gw.Logger = slog.New(slog.NewJSONHandler(&buf, nil))

			res, err := gw.Fetch(context.Background(), http.MethodGet, sellerWith(t, status).URL+"/content", nil, nil)
			if !errors.Is(err, ErrUpstream) {
				t.Fatalf("want ErrUpstream, got %v", err)
			}
			if res != nil {
				t.Fatalf("want nil result on post-sign %d, got %+v", status, res)
			}
			if got := payments.Load(); got != 0 {
				t.Fatalf("OnPayment calls = %d, want 0 on post-sign %d", got, status)
			}
			if spent := spendToday(t, gw); spent != 0 {
				t.Fatalf("spend = %d, want 0 on post-sign %d", spent, status)
			}
			if gw.Blocks.Current() == nil {
				t.Fatal("block must survive a post-sign failure")
			}
			if gw.lastFetchError == nil || gw.lastFetchError.Code != "upstream_error" {
				t.Fatalf("lastFetchError = %+v, want code upstream_error", gw.lastFetchError)
			}
			// 37.1/F4: one attempt = exactly one audit line with the real amount.
			lines, _ := decodeAuditLines(t, buf.String())
			if len(lines) != 1 {
				t.Fatalf("audit lines = %d, want exactly 1 per attempt", len(lines))
			}
			if m := lines[0]; m["outcome"] != "failed:upstream_error" || m["amount_micro"] != float64(10_000) {
				t.Fatalf("wrong upstream_error audit line: %v", m)
			}
		})
	}
}

// ad 1: a seller that raises the price above the approved amount must NOT be
// paid silently — the gateway returns a price_changed re-approval request.
func TestPriceChangeReasksNotPays(t *testing.T) {
	gw, payments := newSettleGateway(t)
	// Approved $0.005 (5000); the seller now asks $0.01 (10000).
	_, err := gw.FetchWithOverride(context.Background(), http.MethodGet, sellerWith(t, http.StatusOK).URL+"/content", nil, nil, 5_000, false)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "price_changed" {
		t.Fatalf("want price_changed, got %v", err)
	}
	if !perr.CanOverride || perr.AmountMicro != 10_000 {
		t.Fatalf("want overridable new amount 10000, got %+v", perr)
	}
	if got := payments.Load(); got != 0 {
		t.Fatalf("OnPayment calls = %d on price change, want 0", got)
	}
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 0 {
		t.Fatalf("signed %d times on a price change, want 0", got)
	}
	if spent := spendToday(t, gw); spent != 0 {
		t.Fatalf("spend = %d on price change, want 0", spent)
	}
}

// ad 1: the price_changed refusal is audited exactly once with the new amount.
func TestPriceChangeAuditLine(t *testing.T) {
	gw, _ := newSettleGateway(t)
	var buf bytes.Buffer
	gw.Logger = slog.New(slog.NewJSONHandler(&buf, nil))
	if _, err := gw.FetchWithOverride(context.Background(), http.MethodGet, sellerWith(t, http.StatusOK).URL+"/content", nil, nil, 5_000, false); err == nil {
		t.Fatal("want price_changed")
	}
	lines, _ := decodeAuditLines(t, buf.String())
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 audit line, got %d", len(lines))
	}
	m := lines[0]
	assertAuditKeys(t, m)
	if m["outcome"] != "failed:price_changed" || m["amount_micro"] != float64(10_000) ||
		m["override"] != true {
		t.Fatalf("wrong price_changed audit fields: %v", m)
	}
}

// ad 1: an approval that covers the current price pays normally.
func TestOverridePaysWhenApprovedCoversPrice(t *testing.T) {
	gw, payments := newSettleGateway(t)
	if _, err := gw.FetchWithOverride(context.Background(), http.MethodGet, sellerWith(t, http.StatusOK).URL+"/content", nil, nil, 10_000, false); err != nil {
		t.Fatalf("want paid, got %v", err)
	}
	if got := payments.Load(); got != 1 {
		t.Fatalf("OnPayment calls = %d, want 1", got)
	}
	if spent := spendToday(t, gw); spent != 10_000 {
		t.Fatalf("spend = %d, want 10000", spent)
	}
}

// 36.3: amountErr must refuse the signature instead of paying without
// accounting. Unreachable today (Check/CheckOverride reject non-canonical
// amounts first), so this pins the guard direction, not a live path.
func TestSignRefusesWithoutAccountedAmount(t *testing.T) {
	gw, payments := newSettleGateway(t)
	_, err := gw.signAndRetry(context.Background(), http.MethodGet,
		"https://seller.example/x", nil, nil, nil, nil, 0,
		"GET https://seller.example/x", 10_000, errors.New("bad amount"))
	if !errors.Is(err, ErrSigner) {
		t.Fatalf("err = %v, want ErrSigner", err)
	}
	if payments.Load() != 0 {
		t.Fatalf("payments = %d, want 0", payments.Load())
	}
	if spent := spendToday(t, gw); spent != 0 {
		t.Fatalf("spend = %d, want 0", spent)
	}
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != 0 {
		t.Fatalf("signed %d times without an amount, want 0", got)
	}
}
