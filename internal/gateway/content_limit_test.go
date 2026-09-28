package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 36.1: the seller response body is hostile input (the signature is already
// paid when we read it). toResult must refuse bodies beyond the cap instead of
// buffering them — at most maxSellerContentBytes+1 bytes are ever read.

type infiniteReader struct{}

func (infiniteReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 'x'
	}
	return len(b), nil
}

func responseWithBody(t *testing.T, body io.Reader) *http.Response {
	t.Helper()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(body),
	}
}

func TestToResultTruncatesOversizedBody(t *testing.T) {
	// Just over the cap: refused, nothing usable returned.
	res, err := toResult(responseWithBody(t, io.MultiReader(
		strings.NewReader(strings.Repeat("y", maxSellerContentBytes)),
		strings.NewReader("zz"),
	)))
	if !errors.Is(err, ErrContentTooLarge) {
		t.Fatalf("over-cap body: err = %v, want ErrContentTooLarge", err)
	}
	if res != nil {
		t.Fatal("over-cap body: must return no result, not a truncated one")
	}

	// Exactly at the cap: accepted in full.
	full := strings.Repeat("y", maxSellerContentBytes)
	res, err = toResult(responseWithBody(t, strings.NewReader(full)))
	if err != nil {
		t.Fatalf("at-cap body: %v", err)
	}
	if res == nil || res.Status != http.StatusOK {
		t.Fatalf("at-cap body: want a full result, got %+v", res)
	}

	// An endless stream terminates fast instead of filling memory.
	res, err = toResult(responseWithBody(t, infiniteReader{}))
	if !errors.Is(err, ErrContentTooLarge) {
		t.Fatalf("endless body: err = %v, want ErrContentTooLarge", err)
	}
	if res != nil {
		t.Fatal("endless body: must return no result")
	}
}

// sellerBigContent answers the unsigned hit with a 402 naming amount, and the
// signed retry with retryStatus and a body of exactly size bytes.
func sellerBigContent(t *testing.T, amount string, size int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Payment-Signature") != "" {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(strings.Repeat("z", size)))
			return
		}
		w.Header().Set("Payment-Required", paymentRequiredHeaderWith(amount, usdcBaseSepolia, "eip155:84532"))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// 36.1: the seller settled (2xx) but sent more than we can hold. The money left
// the wallet, so the spend stays recorded — and the agent gets an explicit
// content_too_large instead of truncated content.
func TestOversizedSellerContentIsSurfacedNotDelivered(t *testing.T) {
	gw, payments := newSettleGateway(t)
	var buf bytes.Buffer
	gw.Logger = slog.New(slog.NewJSONHandler(&buf, nil))

	_, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerBigContent(t, "10000", 2*maxSellerContentBytes).URL+"/content", nil, nil)

	if !errors.Is(err, ErrContentTooLarge) {
		t.Fatalf("err = %v, want ErrContentTooLarge", err)
	}
	if code := auditErrorCode(err); code != "content_too_large" {
		t.Fatalf("audit code = %q, want content_too_large", code)
	}
	if payments.Load() != 1 {
		t.Fatalf("payments = %d, want 1 (the seller settled)", payments.Load())
	}
	if spent := spendToday(t, gw); spent != 10_000 {
		t.Fatalf("spend = %d, want 10000 (settled money is recorded)", spent)
	}
	if e := gw.lastError(); e == nil || e.Code != "content_too_large" || e.CanOverride {
		t.Fatalf("last_fetch_error = %+v, want non-overridable content_too_large", e)
	}
	// 37.1/F4: one attempt = exactly one audit line (before the retry below).
	lines, _ := decodeAuditLines(t, buf.String())
	if len(lines) != 1 {
		t.Fatalf("audit lines = %d, want exactly 1 per attempt", len(lines))
	}
	if m := lines[0]; m["outcome"] != "failed:content_too_large" || m["amount_micro"] != float64(10_000) {
		t.Fatalf("wrong content_too_large audit line: %v", m)
	}

	// Nothing truncated was cached: the immediate retry is a duplicate, not a
	// success with partial content.
	if _, err := gw.Fetch(context.Background(), http.MethodGet,
		sellerBigContent(t, "10000", 2*maxSellerContentBytes).URL+"/content", nil, nil); err == nil {
		t.Fatal("retry must not serve truncated content as success")
	}
}
