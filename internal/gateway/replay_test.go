package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gateway/internal/cdp"
	"gateway/internal/policy"
	"gateway/internal/spend"
)

// recordingSigner counts signing attempts (T1: must stay zero on denials).
type recordingSigner struct{ signCalls atomic.Int64 }

func (s *recordingSigner) Address() string              { return "0xe6D2863Eb960a980eC3714f85568f974d03cE04E" }
func (s *recordingSigner) UserID() string               { return "test-user" }
func (s *recordingSigner) AccessToken() (string, error) { return "tok", nil }
func (s *recordingSigner) WalletSecret() (*cdp.WalletSecret, error) {
	s.signCalls.Add(1)
	return nil, nil
}

func newGateway(t *testing.T, daily, perReq int64) (*Gateway, *recordingSigner) {
	t.Helper()
	dir := t.TempDir()
	p := policy.Default()
	p.DailyCapMicro = daily
	gw := &Gateway{Spend: spend.NewTracker(dir), Signer: &recordingSigner{}, AllowPrivate: true}
	gw.SetPolicy(p)
	return gw, gw.Signer.(*recordingSigner)
}

// T1: adversarial PAYMENT-REQUIRED payloads must be denied before signing.
func TestSecurityMaliciousRequirements(t *testing.T) {
	cases := []struct {
		name    string
		amount  string
		asset   string
		network string
		want    string
	}{
		{"huge price", "99999999999999999999", usdcBaseSepolia, "eip155:84532", "invalid_amount"},
		{"negative amount", "-5", usdcBaseSepolia, "eip155:84532", "invalid_amount"},
		{"float amount", "1e9", usdcBaseSepolia, "eip155:84532", "invalid_amount"},
		{"zero amount", "0", usdcBaseSepolia, "eip155:84532", "invalid_amount"},
		{"scam token", "10000", "0xDeadBeef00000000000000000000000000000001", "eip155:84532", "network_denied"},
		{"unknown chain", "10000", usdcBaseSepolia, "eip155:137", "network_denied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gw, signer := newGateway(t, 5_000_000, 250_000)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Payment-Required", paymentRequiredHeaderWith(tc.amount, tc.asset, tc.network))
				w.WriteHeader(http.StatusPaymentRequired)
			}))
			defer upstream.Close()

			_, err := gw.Fetch(context.Background(), http.MethodGet, upstream.URL+"/content", nil, nil)
			var perr *PolicyError
			if !errors.As(err, &perr) || perr.Code != tc.want {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
			if signer.signCalls.Load() != 0 {
				t.Fatalf("signed %d times on malicious requirements", signer.signCalls.Load())
			}
		})
	}
}

// T4: identical target inside the dedup window must be rejected without a
// second signature, even if the first attempt succeeded.
func TestSecurityReplayWindow(t *testing.T) {
	gw, _ := newGateway(t, 5_000_000, 250_000)
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	gw.now = func() time.Time { return base }

	key := "GET https://seller.example/x"
	if _, err := gw.reserve(key); err != nil {
		t.Fatalf("first call must reserve, got %v", err)
	}
	gw.markSigned(key)
	if _, err := gw.reserve(key); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("want duplicate_payment inside window, got %v", err)
	}
	gw.now = func() time.Time { return base.Add(6 * time.Second) }
	if _, err := gw.reserve(key); err != nil {
		t.Fatalf("outside window must pass again, got %v", err)
	}
	// A reservation released without settling must not block a retry.
	other := "GET https://seller.example/y"
	seq, err := gw.reserve(other)
	if err != nil {
		t.Fatalf("reserve other: %v", err)
	}
	gw.release(other, seq)
	if _, err := gw.reserve(other); err != nil {
		t.Fatalf("released key must be reservable again, got %v", err)
	}
}

// 30.2b audit (HOLE): two identical paid fetches in flight at the same time
// must pay once. The guard used to be check-then-act — checked here, marked only
// after signing — so both flows could reach the signer and charge twice.
func TestConcurrentIdenticalFetchesPayOnce(t *testing.T) {
	gw, payments := newSettleGateway(t)
	seller := sellerWith(t, http.StatusOK)
	url := seller.URL + "/content"

	const n = 4
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = gw.Fetch(context.Background(), http.MethodGet, url, nil, nil)
		}()
	}
	close(start)
	wg.Wait()

	if got := payments.Load(); got != 1 {
		t.Fatalf("payments = %d, want exactly 1 for %d concurrent identical fetches", got, n)
	}
}

// 33.x: parallel identical requests are duplicates, not budget failures. The cap
// fits exactly ONE payment, so a loser that reached the spend check would be
// reported as budget_exceeded — the reason the user never asked for. Reserve
// first, so every loser is duplicate_payment.
func TestConcurrentIdenticalNeverReportsBudgetExceeded(t *testing.T) {
	gw, payments := newSettleGateway(t)
	p := policy.Default()
	p.DailyCapMicro = 10_000 // exactly one 10000-micro payment
	gw.SetPolicy(p)
	url := sellerWith(t, http.StatusOK).URL + "/content"

	const n = 4
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = gw.Fetch(context.Background(), http.MethodGet, url, nil, nil)
		}(i)
	}
	close(start)
	wg.Wait()

	if got := payments.Load(); got != 1 {
		t.Fatalf("payments = %d, want 1", got)
	}
	won := 0
	for i, err := range errs {
		if err == nil {
			won++
			continue
		}
		if code := auditErrorCode(err); code != "duplicate_payment" {
			t.Fatalf("loser %d code = %q, want duplicate_payment (err=%v)", i, code, err)
		}
	}
	if won != 1 {
		t.Fatalf("successes = %d, want 1", won)
	}
}

// Helpers shared with existing tests live in socket_test.go (same package).
var (
	_ = policy.Default
	_ = spend.NewTracker
)

func paymentRequiredHeaderWith(amount, asset, network string) string {
	body := `{"x402Version":2,"accepts":[{"scheme":"exact","network":"` + network +
		`","asset":"` + asset + `","amount":"` + amount +
		`","payTo":"0x19c1d70Df1F5179CfD015A88Acc7371E203B092C","maxTimeoutSeconds":600,"extra":{"name":"USD Coin","version":"2"}}]}`
	return base64Encode([]byte(body))
}

const usdcBaseSepolia = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"

func base64Encode(b []byte) string {
	const enc = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		var chunk [3]byte
		copy(chunk[:], b[i:])
		n := 3
		if len(b)-i < 3 {
			n = len(b) - i
		}
		v := uint32(chunk[0])<<16 | uint32(chunk[1])<<8 | uint32(chunk[2])
		for j := 0; j <= n; j++ {
			out.WriteByte(enc[(v>>uint(18-6*j))&0x3f])
		}
		for j := n; j < 3; j++ {
			out.WriteByte('=')
		}
	}
	return out.String()
}
