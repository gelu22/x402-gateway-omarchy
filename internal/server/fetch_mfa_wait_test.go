package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gateway/internal/cdp"
)

// waitSigner is testSigner with a real P-256 secret, so signing actually
// reaches the fake CDP endpoint instead of failing on a nil key.
type waitSigner struct{ ws *cdp.WalletSecret }

func (s *waitSigner) Address() string              { return "0x19c1d70Df1F5179CfD015A88Acc7371E203B092C" }
func (s *waitSigner) UserID() string               { return "test-user" }
func (s *waitSigner) AccessToken() (string, error) { return "tok", nil }
func (s *waitSigner) WalletSecret() (*cdp.WalletSecret, error) {
	return s.ws, nil
}

// 30.2b end to end: a fetch blocked by CDP's mfa_required parks instead of
// failing, and the code completes the payment in the SAME response. An
// identical retry is then served from the cache without a second charge.
func TestFetchWaitsForMfaAndServesThePaidContent(t *testing.T) {
	defer func(d time.Duration) { mfaWaitTimeout = d }(mfaWaitTimeout)
	mfaWaitTimeout = 5 * time.Second

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	gw := newTestGateway(t)
	gw.Signer = &waitSigner{ws: cdp.NewWalletSecret("tws", time.Time{}, nil, key)}
	gw.Client = cdp.NewClient("test-project")

	var requireMFA, signs, paid atomic.Int32
	requireMFA.Store(1)
	fakeSign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		signs.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if requireMFA.Load() == 1 {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errorType":"mfa_required","errorMessage":"MFA required"}`))
			return
		}
		_, _ = w.Write([]byte(`{"signature":"0x` + strings.Repeat("a", 130) + `"}`))
	}))
	t.Cleanup(fakeSign.Close)
	gw.Client.BaseURL = fakeSign.URL

	seller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Payment-Signature") != "" {
			paid.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("PAID CONTENT"))
			return
		}
		w.Header().Set("Payment-Required", paymentRequiredHeader("10000"))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(seller.Close)

	socketPath := startTestServer(t, gw, &mockMFAServer{})
	client := testClient(socketPath)
	reqBody := `{"method":"GET","url":"` + seller.URL + `/content"}`

	type reply struct {
		status int
		b64    string
		raw    string
	}
	done := make(chan reply, 1)
	go func() {
		resp, err := client.Post("http://localhost/fetch", "application/json", strings.NewReader(reqBody))
		if err != nil {
			done <- reply{status: -1, raw: err.Error()}
			return
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var out struct {
			BodyB64 string `json:"body_b64"`
		}
		_ = json.Unmarshal(raw, &out)
		done <- reply{status: resp.StatusCode, b64: out.BodyB64, raw: string(raw)}
	}()

	waitForMfaBlock(t, client)

	requireMFA.Store(0)
	verify, err := client.Post("http://localhost/mfa/verify/submit", "application/json",
		strings.NewReader(`{"mfa_code":"123456"}`))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	verify.Body.Close()
	if verify.StatusCode != http.StatusOK {
		t.Fatalf("verify status = %d, want 200", verify.StatusCode)
	}

	select {
	case got := <-done:
		if got.status != http.StatusOK {
			t.Fatalf("fetch status = %d, want 200: %s", got.status, got.raw)
		}
		decoded, err := base64.StdEncoding.DecodeString(got.b64)
		if err != nil || string(decoded) != "PAID CONTENT" {
			t.Fatalf("body_b64 = %q (err %v), want the paid content", got.b64, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the fetch did not complete after verification")
	}
	if signs.Load() != 2 {
		t.Fatalf("sign attempts = %d, want 2 (mfa_required, then success)", signs.Load())
	}
	if paid.Load() != 1 {
		t.Fatalf("paid retries = %d, want 1", paid.Load())
	}

	// Identical retry inside resultTTL: content from the cache, no second charge.
	resp, err := client.Post("http://localhost/fetch", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("cached retry: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cached retry status = %d, want 200: %s", resp.StatusCode, raw)
	}
	var out struct {
		BodyB64 string `json:"body_b64"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("cached retry body: %v", err)
	}
	if decoded, err := base64.StdEncoding.DecodeString(out.BodyB64); err != nil || string(decoded) != "PAID CONTENT" {
		t.Fatalf("cached retry body_b64 = %q, want the paid content", out.BodyB64)
	}
	if paid.Load() != 1 {
		t.Fatalf("paid retries after the cached retry = %d, want 1 (no double charge)", paid.Load())
	}
}

func waitForMfaBlock(t *testing.T, client *http.Client) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://localhost/status")
		if err == nil {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var st struct {
				LastFetchError *struct {
					Code string `json:"code"`
				} `json:"last_fetch_error"`
			}
			if json.Unmarshal(raw, &st) == nil && st.LastFetchError != nil && st.LastFetchError.Code == "mfa_required" {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon never recorded mfa_required for the parked fetch")
}
