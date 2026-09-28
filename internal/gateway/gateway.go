// Package gateway orchestrates the paid-fetch flow: request → 402 → policy →
// sign (CDP) → retry. Errors are typed so the transport layer can map them to
// CONTRACTS §1 codes without string matching.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"gateway/internal/cdp"
	"gateway/internal/policy"
	"gateway/internal/spend"
)

const (
	dedupWindow = 5 * time.Second // T4: identical target within window = duplicate
	// FetchTimeout bounds upstream seller requests; cmd/gateway reuses it
	// for the injected HTTP client (008.1: single source, was copy-pasted).
	FetchTimeout = 30 * time.Second
)

// Sentinel errors mapped by the transport layer.
var (
	ErrPaused          = errors.New("paused")
	ErrDuplicate       = errors.New("duplicate_payment")
	ErrUpstream        = errors.New("upstream_error")
	ErrSigner          = errors.New("signer_error")
	ErrBadTarget       = errors.New("bad_target")
	ErrNoRequirements  = errors.New("no_requirements")
	ErrContentTooLarge = errors.New("content_too_large")
)

// PolicyError carries the CONTRACTS code (invalid_amount | budget_exceeded |
// network_denied | price_changed). budget_exceeded and price_changed carry
// AmountMicro + CanOverride=true: the payment waits for explicit user approval
// (higher budget, or a seller price raise above the approved amount) instead of
// failing hard or paying silently.
type PolicyError struct {
	Code        string
	AmountMicro int64
	CanOverride bool
}

func (e *PolicyError) Error() string { return e.Code }

// Signer abstracts session state (static in 002.1, Manager in 002.2).
type Signer interface {
	Address() string
	UserID() string
	AccessToken() (string, error)
	WalletSecret() (*cdp.WalletSecret, error)
}

// FetchResult is a proxied response (CONTRACTS §1 success shape).
type FetchResult struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	BodyB64 string            `json:"body_b64"`
}

// Gateway is the paid-fetch orchestrator.
type Gateway struct {
	Client *cdp.Client
	Signer Signer
	Spend  *spend.Tracker
	Paused atomic.Bool
	HTTP   *http.Client

	// PolicyPath enables hot-reload of policy.json before every fetch.
	// A corrupt/unreadable file keeps the previous policy (never loosens caps).
	PolicyPath string
	policy     atomic.Pointer[policy.Policy]

	// AllowPrivate disables the SSRF local/private-target guard.
	// Production daemons must leave it false; tests use it for httptest.
	AllowPrivate bool

	now func() time.Time // injectable clock (tests)

	// SessionState reports the session lifecycle state for /status.
	SessionState func() string
	// MFAState reports MFA enrollment for /status (nil = unknown).
	MFAState func() (enrolled bool, method string)
	// OnPayment fires after a successful paid fetch (telemetry hook).
	OnPayment func(amountMicro int64, domain string)
	// Sellers is the seller trust registry (013.2, TOFU). Nil = gates
	// skipped (minimal embeds/tests; production always wires it).
	Sellers *SellerRegistry
	// Logger receives the money audit trail (011.1, file-backed JSONL audit.log).
	// Nil = silent (tests, minimal embeds); production wires the audit file in main.
	Logger *slog.Logger
	// Balance fetches the wallet USDC balance (nil = skip).
	Balance *BalanceFetcher
	// Blocks persists denial reasons and dedups desktop notifications.
	Blocks *BlockTracker
	// PaymentNetwork is the CAIP-2 network used for payments/balance (e.g., eip155:84532).
	PaymentNetwork string

	// Ssrf provides dial-time SSRF protection (DNS rebinding prevention).
	Ssrf *SsrfGuard

	mu             sync.Mutex
	lastSign       map[string]signMark
	resSeq         reservation
	lastFetchError *FetchErrorInfo
}

// clock returns the injected or real clock.
func (g *Gateway) clock() time.Time {
	if g.now != nil {
		return g.now()
	}
	return time.Now()
}

// SetPolicy stores the initial policy snapshot.
func (g *Gateway) SetPolicy(p *policy.Policy) { g.policy.Store(p) }

// CurrentPolicy returns the active policy, hot-reloading from PolicyPath.
// PolicyPath points at the policy file (see cmd/gateway/build.go), so the
// directory — not the path itself — is what policy.Load expects.
func (g *Gateway) CurrentPolicy() *policy.Policy {
	if g.PolicyPath != "" {
		if p, err := policy.Load(filepath.Dir(g.PolicyPath)); err == nil {
			g.policy.Store(p)
		}
	}
	return g.policy.Load()
}

// Fetch performs the possibly-paid request.
func (g *Gateway) Fetch(ctx context.Context, method, target string, body []byte, headers map[string]string) (*FetchResult, error) {
	return g.doFetch(ctx, method, target, body, headers, 0, false)
}

// FetchWithOverride performs a user-approved over-budget fetch, bypassing
// the daily-budget check. overrideAmountMicro must be > 0 and >= the amount
// requested by the endpoint (sanity: approval covers the charge).
func (g *Gateway) FetchWithOverride(ctx context.Context, method, target string, body []byte, headers map[string]string, overrideAmountMicro int64, approveSeller bool) (*FetchResult, error) {
	if overrideAmountMicro <= 0 {
		return nil, fmt.Errorf("override amount must be positive")
	}
	return g.doFetch(ctx, method, target, body, headers, overrideAmountMicro, approveSeller)
}

// recordBlock stores the denial reason and fires the once-per-day desktop
// notification for budget exhaustion. Never fails the fetch path.
func (g *Gateway) recordBlock(reason, amount, target string) {
	if g.Blocks == nil {
		return
	}
	domain := ""
	if u, err := url.Parse(target); err == nil {
		domain = u.Hostname()
	}
	amountMicro, _ := strconv.ParseInt(amount, 10, 64)
	rec := BlockRecord{
		Reason:      reason,
		AmountMicro: amountMicro,
		Domain:      domain,
	}
	g.Blocks.Record(rec)
	if reason == "budget_exceeded" {
		spend, _ := g.Spend.Today()
		cap := g.CurrentPolicy().DailyCapMicro
		body := budgetExhaustedBody(spend, cap)
		// Reserve the day's slot before notifying: under a retry burst the
		// reservation is the only thing that keeps this to one notification.
		if g.Blocks.TryMarkNotified() {
			go desktopNotify("x402 Gateway — daily budget exhausted", body)
		}
	}
}

func microToUSDC(micro int64) float64 { return float64(micro) / 1_000_000 }
