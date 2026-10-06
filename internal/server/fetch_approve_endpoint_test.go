package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gateway/internal/cdp"
	"gateway/internal/gateway"
)

// TestFetchApproveGuards (49.4-daemon): a malformed approve is 400 and an
// unknown id is 404 — before any sudo gate or signing.
func TestFetchApproveGuards(t *testing.T) {
	_, sock := permsServer(t, true)
	// missing id
	resp, err := testClient(sock).Post("http://localhost/fetch-approve", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing id: status = %d, want 400", resp.StatusCode)
	}
	// unknown id
	resp2, err := testClient(sock).Post("http://localhost/fetch-approve", "application/json", bytes.NewReader([]byte(`{"id":"nope"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown id: status = %d, want 404", resp2.StatusCode)
	}
}

// approveSigner is a local settle signer: the package testSigner returns no
// wallet secret, so it cannot complete a signed retry.
type approveSigner struct{ ws *cdp.WalletSecret }

func (s *approveSigner) Address() string              { return "0xe6D2863Eb960a980eC3714f85568f974d03cE04E" }
func (s *approveSigner) UserID() string               { return "test-user" }
func (s *approveSigner) AccessToken() (string, error) { return "tok", nil }
func (s *approveSigner) WalletSecret() (*cdp.WalletSecret, error) {
	return s.ws, nil
}

// countingSeller answers the unsigned probe with a 402 and counts a signed retry.
func countingSeller(t *testing.T, signed *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Payment-Signature") != "" {
			signed.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		w.Header().Set("Payment-Required", paymentRequiredHeader("10000"))
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestFetchApprovePaysTheNamedIDNotTheNewerRefusal (51.3): two refused
// payments, same amount, different URLs. Approving an id pays that URL only.
// A handler that picked the last blocked entry would sign the other seller.
func TestFetchApprovePaysTheNamedIDNotTheNewerRefusal(t *testing.T) {
	t.Setenv("GATEWAY_NOTIFY", "0")
	for _, which := range []string{"older", "newer"} {
		t.Run(which, func(t *testing.T) {
			gw, sock := permsServer(t, true)
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			gw.Signer = &approveSigner{ws: cdp.NewWalletSecret("test-secret", time.Time{}, nil, key)}
			gw.Sellers = gateway.NewSellerRegistry(t.TempDir())
			gw.Client = cdp.NewClient("test-project")
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"signature":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
			}))
			t.Cleanup(fake.Close)
			gw.Client.BaseURL = fake.URL

			var olderSigned, newerSigned atomic.Int32
			older := countingSeller(t, &olderSigned)
			newer := countingSeller(t, &newerSigned)
			client := testClient(sock)
			for _, u := range []string{older.URL + "/a", newer.URL + "/b"} {
				resp, err := client.Post("http://localhost/fetch", "application/json",
					bytes.NewReader([]byte(`{"method":"GET","url":"`+u+`"}`)))
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
			}
			if olderSigned.Load() != 0 || newerSigned.Load() != 0 {
				t.Fatal("refusals must not send Payment-Signature")
			}
			st, err := client.Get("http://localhost/status")
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := io.ReadAll(st.Body)
			st.Body.Close()
			var body struct {
				Blocked []struct {
					ID  string `json:"id"`
					URL string `json:"url"`
				} `json:"blocked"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Blocked) != 2 {
				t.Fatalf("blocked = %d, want 2: %s", len(body.Blocked), raw)
			}
			wantURL := older.URL + "/a"
			if which == "newer" {
				wantURL = newer.URL + "/b"
			}
			id := ""
			for _, b := range body.Blocked {
				if b.URL == wantURL {
					id = b.ID
				}
			}
			if id == "" {
				t.Fatalf("status has no entry for %s: %s", wantURL, raw)
			}
			resp, err := client.Post("http://localhost/fetch-approve", "application/json",
				bytes.NewReader([]byte(`{"id":"`+id+`"}`)))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if which == "older" {
				if olderSigned.Load() != 1 || newerSigned.Load() != 0 {
					t.Fatalf("older id signed older=%d newer=%d, want 1 and 0", olderSigned.Load(), newerSigned.Load())
				}
			} else if newerSigned.Load() != 1 || olderSigned.Load() != 0 {
				t.Fatalf("newer id signed older=%d newer=%d, want 0 and 1", olderSigned.Load(), newerSigned.Load())
			}
		})
	}
}

// 54.5: approve replays under the agent label that caused the block.
func TestApproveReplaysUnderOriginalAgent(t *testing.T) {
	t.Setenv("GATEWAY_NOTIFY", "0")
	gw, sock := permsServer(t, true)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	gw.Signer = &approveSigner{ws: cdp.NewWalletSecret("test-secret", time.Time{}, nil, key)}
	gw.Sellers = gateway.NewSellerRegistry(t.TempDir())
	gw.Client = cdp.NewClient("test-project")
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"signature":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	}))
	t.Cleanup(fake.Close)
	gw.Client.BaseURL = fake.URL

	var buf bytes.Buffer
	gw.Logger = slog.New(slog.NewJSONHandler(&buf, nil))

	var signed atomic.Int32
	seller := countingSeller(t, &signed)
	client := testClient(sock)
	req, err := http.NewRequest(http.MethodPost, "http://localhost/fetch",
		bytes.NewReader([]byte(`{"method":"GET","url":"`+seller.URL+`/x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gateway-Agent", "codex")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	st, err := client.Get("http://localhost/status")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(st.Body)
	st.Body.Close()
	var body struct {
		Blocked []struct {
			ID    string `json:"id"`
			Agent string `json:"agent"`
		} `json:"blocked"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Blocked) != 1 || body.Blocked[0].Agent != "codex" {
		t.Fatalf("blocked = %+v, want agent=codex", body.Blocked)
	}
	buf.Reset()
	aresp, err := client.Post("http://localhost/fetch-approve", "application/json",
		bytes.NewReader([]byte(`{"id":"`+body.Blocked[0].ID+`"}`)))
	if err != nil {
		t.Fatal(err)
	}
	aresp.Body.Close()
	if signed.Load() != 1 {
		t.Fatalf("signed = %d, want 1", signed.Load())
	}
	found := false
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		if m["msg"] == "payment audit" && m["outcome"] == "paid" {
			if m["agent"] != "codex" {
				t.Fatalf("paid agent = %v, want codex", m["agent"])
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("no paid audit line; log=%s", buf.String())
	}
}
