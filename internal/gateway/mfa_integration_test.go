package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gateway/internal/cdp"
	"gateway/internal/policy"
	"gateway/internal/session"
	"gateway/internal/spend"
)

const (
	mfaUserID   = "00000000-0000-4000-8000-000000000000"
	mfaEVMAddr  = "0x3caabbF86C8F53C3CdCB4DF3BE0Fa68FCe33630F"
	mfaValidTil = "2030-01-01T00:00:00Z"
)

// mfaFakeCDP emulates the CDP surface this flow touches: token refresh, TWS
// creation, TOTP enroll/verify and typed-data signing. The sign endpoint keeps
// rejecting with mfa_required until verify/submit succeeds — that is the whole
// point: the daemon must fail closed, then pay after out-of-band verification.
type mfaFakeCDP struct {
	verified     atomic.Bool
	enrollSubmit atomic.Int32
	signCalls    atomic.Int32
}

func (f *mfaFakeCDP) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/auth/refresh"):
			writeCDPJSON(w, http.StatusOK, map[string]any{
				"endUser": map[string]any{
					"userId":                mfaUserID,
					"authenticationMethods": []map[string]string{{"type": "email", "email": "operator@example.com"}},
				},
				"accessToken":  "access-1",
				"validUntil":   mfaValidTil,
				"refreshToken": "refresh-2",
			})
		case r.Method == http.MethodPut && strings.HasSuffix(p, "/wallet-secrets"):
			var in struct {
				WalletSecretID string `json:"walletSecretId"`
				PublicKey      string `json:"publicKey"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.WalletSecretID == "" || in.PublicKey == "" {
				writeCDPJSON(w, http.StatusBadRequest, map[string]string{"errorType": "invalid_request"})
				return
			}
			writeCDPJSON(w, http.StatusOK, map[string]string{
				"walletSecretId": in.WalletSecretID,
				"validUntil":     mfaValidTil,
			})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/mfa/enroll/totp/initiate"):
			writeCDPJSON(w, http.StatusOK, map[string]string{
				"authUrl": "otpauth://totp/Gateway?secret=JBSWY3DPEHPK3PXP",
				"secret":  "JBSWY3DPEHPK3PXP",
			})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/mfa/enroll/totp/submit"):
			f.enrollSubmit.Add(1)
			writeCDPJSON(w, http.StatusOK, map[string]any{})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/mfa/verify/totp/init"):
			writeCDPJSON(w, http.StatusOK, map[string]any{})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/mfa/verify/totp/submit"):
			f.verified.Store(true)
			writeCDPJSON(w, http.StatusOK, map[string]any{})
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/evm/sign/typed-data"):
			f.signCalls.Add(1)
			if !f.verified.Load() {
				writeCDPJSON(w, http.StatusForbidden, map[string]string{
					"errorType": "mfa_required", "errorMessage": "MFA required",
				})
				return
			}
			writeCDPJSON(w, http.StatusOK, map[string]string{
				"signature": "0x" + strings.Repeat("a", 130),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeCDPJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// mfaGateway wires a gateway whose Signer is a real session.Manager over the
// fake CDP, so the enroll → verify → pay flow crosses every real layer.
func mfaGateway(t *testing.T, f *mfaFakeCDP, store *session.Store) (*Gateway, *atomic.Int32) {
	t.Helper()
	client := cdp.NewClient("test-project")
	client.BaseURL = f.start(t).URL

	mgr, err := session.New(client, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	gw := &Gateway{
		Spend:        spend.NewTracker(t.TempDir()),
		Signer:       mgr,
		AllowPrivate: true,
		Blocks:       NewBlockTracker(t.TempDir(), nil),
		Client:       client,
	}
	p := policy.Default()
	p.DailyCapMicro = 5_000_000
	gw.SetPolicy(p)

	var payments atomic.Int32
	gw.OnPayment = func(int64, string) { payments.Add(1) }
	return gw, &payments
}

func seededStore(t *testing.T) *session.Store {
	t.Helper()
	store := session.NewStore(t.TempDir())
	if err := store.Save(&session.Data{
		UserID: mfaUserID, Email: "operator@example.com", EVMAddress: mfaEVMAddr, RefreshToken: "refresh-1",
	}); err != nil {
		t.Fatal(err)
	}
	return store
}

// TestMfaFullFlowEnrollVerifyPay is the live sequence the panel drives:
// enrollment, then a fail-closed 402/denial, then verification, then payment.
func TestMfaFullFlowEnrollVerifyPay(t *testing.T) {
	f := &mfaFakeCDP{}
	gw, payments := mfaGateway(t, f, seededStore(t))
	mgr := gw.Signer.(*session.Manager)
	ctx := context.Background()

	authURL, secret, qr, err := mgr.MfaEnrollInit(ctx)
	if err != nil {
		t.Fatalf("enroll init: %v", err)
	}
	if !strings.HasPrefix(authURL, "otpauth://") || secret == "" || !strings.HasPrefix(qr, "data:image/png;base64,") {
		t.Fatalf("enroll init incomplete: url=%q secret=%q qr=%.20q", authURL, secret, qr)
	}
	if err := mgr.MfaEnrollSubmit(ctx, "123456"); err != nil {
		t.Fatalf("enroll submit: %v", err)
	}

	url := sellerWith(t, http.StatusOK).URL + "/content"

	_, err = gw.Fetch(ctx, http.MethodGet, url, nil, nil)
	var perr *PolicyError
	if !errors.As(err, &perr) || perr.Code != "mfa_required" {
		t.Fatalf("want mfa_required, got %v", err)
	}
	if perr.CanOverride {
		t.Fatal("mfa_required must not be budget-overridable")
	}
	if payments.Load() != 0 {
		t.Fatalf("OnPayment = %d while MFA missing, want 0", payments.Load())
	}
	if gw.lastFetchError == nil || gw.lastFetchError.Code != "mfa_required" {
		t.Fatalf("last_fetch_error = %+v, want mfa_required", gw.lastFetchError)
	}

	if err := mgr.MfaVerifyInit(ctx); err != nil {
		t.Fatalf("verify init: %v", err)
	}
	if err := mgr.MfaVerifySubmit(ctx, "654321"); err != nil {
		t.Fatalf("verify submit: %v", err)
	}

	res, err := gw.Fetch(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		t.Fatalf("fetch after verify: %v", err)
	}
	if res.Status != http.StatusOK || payments.Load() != 1 {
		t.Fatalf("want settled retry, got status=%d payments=%d", res.Status, payments.Load())
	}
}

// TestMfaEnrollSubmitRejectsBadCode: a malformed code never reaches CDP.
func TestMfaEnrollSubmitRejectsBadCode(t *testing.T) {
	f := &mfaFakeCDP{}
	gw, _ := mfaGateway(t, f, seededStore(t))
	mgr := gw.Signer.(*session.Manager)

	if err := mgr.MfaEnrollSubmit(context.Background(), "12345"); err == nil {
		t.Fatal("5-digit code: want error, got nil")
	}
	if got := f.enrollSubmit.Load(); got != 0 {
		t.Fatalf("CDP enroll submit called %d times for a malformed code, want 0", got)
	}
}

// TestMfaLoggedOutIsNotSignedIn: without a session the flow stops before CDP.
func TestMfaLoggedOutIsNotSignedIn(t *testing.T) {
	f := &mfaFakeCDP{}
	gw, _ := mfaGateway(t, f, session.NewStore(t.TempDir()))
	mgr := gw.Signer.(*session.Manager)

	if _, _, _, err := mgr.MfaEnrollInit(context.Background()); !errors.Is(err, session.ErrNotSignedIn) {
		t.Fatalf("logged-out enroll init err = %v, want ErrNotSignedIn", err)
	}
}
