package gateway

// Adversarial seller harness (016.2): the seller is the attacker.
import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// evilSeller serves a caller-built 402, then retryStatus on the signed retry.
func evilSeller(t *testing.T, header string, retryStatus int, onRetry func()) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Payment-Signature") != "" {
			if onRetry != nil {
				onRetry()
			}
			w.WriteHeader(retryStatus)
			_, _ = w.Write([]byte("x"))
			return
		}
		if header != "" {
			w.Header().Set("Payment-Required", header)
		}
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func validHeader(amount string) string {
	return paymentRequiredHeaderWith(amount, usdcBaseSepolia, "eip155:84532")
}

func assertSigns(t *testing.T, gw *Gateway, want int32) {
	t.Helper()
	if got := gw.Signer.(*settleSigner).signCalls.Load(); got != want {
		t.Fatalf("signatures = %d, want %d", got, want)
	}
}

func assertNoSpend(t *testing.T, gw *Gateway) {
	t.Helper()
	spent, err := gw.Spend.Today()
	if err != nil {
		t.Fatal(err)
	}
	if spent != 0 {
		t.Fatalf("spend = %d, want 0", spent)
	}
}

// Double 402 after signing: exactly one signature, zero spend, one audit line.
func TestAdversarialDouble402AfterSign(t *testing.T) {
	gw, _ := newSettleGateway(t)
	var buf bytes.Buffer
	gw.Logger = slog.New(slog.NewJSONHandler(&buf, nil))
	srv := evilSeller(t, validHeader("20000"), http.StatusPaymentRequired, nil)

	_, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil)
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("want ErrUpstream on double 402, got %v", err)
	}
	assertSigns(t, gw, 1)
	assertNoSpend(t, gw)
	lines, _ := decodeAuditLines(t, buf.String())
	if len(lines) != 1 {
		t.Fatalf("want exactly 1 audit line, got %d", len(lines))
	}
}

// Hanging signed retry: fail-closed fast, no hang (test time is the guard).
func TestAdversarialRetryTimeout(t *testing.T) {
	gw, _ := newSettleGateway(t)
	gw.HTTP = &http.Client{Timeout: 200 * time.Millisecond}
	srv := evilSeller(t, validHeader("20000"), http.StatusOK, func() {
		time.Sleep(500 * time.Millisecond)
	})

	start := time.Now()
	_, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil)
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("fetch hung for %v", elapsed)
	}
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("want ErrUpstream on timeout, got %v", err)
	}
	assertSigns(t, gw, 1)
	assertNoSpend(t, gw)
}

// Malformed 402s must fail before signing with zero spend.
func TestAdversarialMalformed402(t *testing.T) {
	cases := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"garbage base64", "!!!not-base64!!!"},
		{"garbage json", "aW52YWxpZA=="}, // "invalid"
		{"empty accepts", "eyJ4NDAyVmVyc2lvbiI6Mn0="},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw, _ := newSettleGateway(t)
			srv := evilSeller(t, tc.header, http.StatusOK, nil)
			_, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil)
			if err == nil {
				t.Fatal("want explicit error, got nil")
			}
			// All malformed-402 paths surface ErrNoRequirements (empty accepts
			// included: no usable requirement exists to classify further).
			if !errors.Is(err, ErrNoRequirements) {
				t.Fatalf("want ErrNoRequirements, got %v", err)
			}
			assertSigns(t, gw, 0)
			assertNoSpend(t, gw)
		})
	}
}

// Absurd amounts: rejected pre-sign, no overflow in spend math.
func TestAdversarialAbsurdAmounts(t *testing.T) {
	cases := []struct {
		amount string
		want   string // expected PolicyError code
	}{
		{"9223372036854775807", "budget_exceeded"},
		{"-5", "invalid_amount"},
		{"0", "invalid_amount"},
		{"1e9", "invalid_amount"},
		{"0x10", "invalid_amount"},
		{"  ", "invalid_amount"},
		{"1,000", "invalid_amount"},
	}
	for _, tc := range cases {
		t.Run("amount="+tc.amount, func(t *testing.T) {
			gw, _ := newSettleGateway(t)
			srv := evilSeller(t, validHeader(tc.amount), http.StatusOK, nil)
			_, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil)
			var perr *PolicyError
			if !errors.As(err, &perr) || perr.Code != tc.want {
				t.Fatalf("want PolicyError %q for amount %q, got %v", tc.want, tc.amount, err)
			}
			assertSigns(t, gw, 0)
			assertNoSpend(t, gw)
		})
	}
}

// Paid replay: second identical fetch in the dedup window fails outright.
func TestAdversarialPaidReplayNoResign(t *testing.T) {
	gw, payments := newSettleGateway(t)
	srv := sellerWith(t, http.StatusOK)
	target := srv.URL + "/content"
	if _, err := gw.Fetch(context.Background(), http.MethodGet, target, nil, nil); err != nil {
		t.Fatalf("first fetch must pay, got %v", err)
	}
	if _, err := gw.Fetch(context.Background(), http.MethodGet, target, nil, nil); err == nil {
		t.Fatal("want duplicate error on replay, got nil")
	}
	assertSigns(t, gw, 1)
	if got := payments.Load(); got != 1 {
		t.Fatalf("payments = %d, want exactly 1 (no double-spend)", got)
	}
	if spent := spendToday(t, gw); spent != 10_000 {
		t.Fatalf("spend = %d, want exactly 10000 (single record)", spent)
	}
}

// Empty 200 documents honest semantics: 2xx means settled, so spend records it.
func TestAdversarialEmpty200Settles(t *testing.T) {
	gw, payments := newSettleGateway(t)
	srv := evilSeller(t, validHeader("20000"), http.StatusOK, nil)
	if _, err := gw.Fetch(context.Background(), http.MethodGet, srv.URL+"/content", nil, nil); err != nil {
		t.Fatalf("empty 200 must succeed, got %v", err)
	}
	assertSigns(t, gw, 1)
	if got := payments.Load(); got != 1 {
		t.Fatalf("payments = %d, want 1", got)
	}
	if spent := spendToday(t, gw); spent != 20_000 {
		t.Fatalf("spend = %d, want 20000", spent)
	}
}
