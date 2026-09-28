// fetch.go — doFetch orchestration + evaluateSeller.
// The signing/retry section is in fetch_sign.go, first-request parsing in fetch_parse.go.
package gateway

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gateway/internal/policy"
	"gateway/internal/x402"
)

// doFetch is the shared implementation for Fetch and FetchWithOverride.
// If overrideAmountMicro > 0, the daily-budget test is skipped (the user
// already approved this over-budget payment in the panel).
func (g *Gateway) doFetch(ctx context.Context, method, target string, body []byte, headers map[string]string, overrideAmountMicro int64, approveSeller bool) (result *FetchResult, err error) {
	var auditAmount int64
	audited := false
	defer func() {
		if err == nil || audited {
			return
		}
		LogPayment(g.Logger, auditAmount, target, "failed:"+auditErrorCode(err), overrideAmountMicro > 0)
	}()

	if g.Paused.Load() {
		return nil, ErrPaused
	}
	if !g.AllowPrivate {
		if err := validateTarget(target); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBadTarget, err)
		}
	}
	g.mu.Lock()
	g.lastFetchError = nil
	g.mu.Unlock()

	key := strings.ToUpper(method) + " " + target
	reservedAt, rerr := g.reserve(key)
	if rerr != nil {
		return nil, rerr
	}
	// A flow that did not settle releases its claim (a retry after a denial must
	// work); a paid flow keeps the mark written by markSigned.
	defer func() {
		if err != nil {
			g.release(key, reservedAt)
		}
	}()

	// --- First request + 402 handling ---
	first, pr, req, err := g.parse402Response(ctx, method, target, body, headers, key)
	if err != nil {
		return nil, err
	}
	if first != nil && pr == nil {
		// Successful non-payment response (nothing signed, so no spend either way).
		res, rerr := toResult(first)
		if rerr != nil {
			g.setLastFetchError("content_too_large", 0, false, target, rerr.Error())
			return nil, fmt.Errorf("%w: %v", ErrContentTooLarge, rerr)
		}
		return res, nil
	}

	// --- Spend + policy check ---
	spendToday, err := g.Spend.Today()
	if err != nil {
		g.setLastFetchError("upstream_error", 0, false, target, err.Error())
		return nil, err
	}
	pol := g.CurrentPolicy()
	var perr error
	if overrideAmountMicro > 0 {
		perr = pol.CheckOverride(req)
	} else {
		perr = pol.Check(req, spendToday)
	}

	// --- Seller trust (013.2, TOFU) ---
	if serr := g.evaluateSeller(target, approveSeller, pol, req, overrideAmountMicro); serr != nil {
		if perr == nil {
			perr = serr
		}
	}

	// --- Policy error handling ---
	if perr != nil {
		amountMicro, _ := strconv.ParseInt(req.Amount, 10, 64)
		if overrideAmountMicro == 0 && perr.Error() == "budget_exceeded" {
			g.recordBlock(perr.Error(), req.Amount, target)
			g.setLastFetchError("budget_exceeded", amountMicro, true, target, perr.Error())
			audited = true
			LogPayment(g.Logger, amountMicro, target, "failed:budget_exceeded", overrideAmountMicro > 0)
			return nil, &PolicyError{Code: "budget_exceeded", AmountMicro: amountMicro, CanOverride: true}
		}
		g.recordBlock(perr.Error(), req.Amount, target)
		g.setLastFetchError(perr.Error(), amountMicro, false, target, perr.Error())
		audited = true
		LogPayment(g.Logger, amountMicro, target, policyOutcome(perr.Error()), overrideAmountMicro > 0)
		if _, isPolicy := perr.(*PolicyError); isPolicy {
			return nil, perr
		}
		return nil, &PolicyError{Code: perr.Error()}
	}

	// --- Override amount check ---
	amountMicro, amountErr := strconv.ParseInt(req.Amount, 10, 64)
	auditAmount = amountMicro
	if overrideAmountMicro > 0 && amountMicro > overrideAmountMicro {
		g.setLastFetchError("price_changed", amountMicro, true, target, "price_changed")
		return nil, &PolicyError{Code: "price_changed", AmountMicro: amountMicro, CanOverride: true}
	}

	// --- Funds pre-check (33.2) ---
	// Advisory, not a gate: block only when the balance is KNOWN and lower than
	// the amount. An RPC failure must not look like "no funds" — settlement
	// stays the fail-closed gate. The balance contract is float64 USDC; the
	// rounding here cannot matter because the real decision is the settle.
	if g.Balance != nil && amountMicro > 0 {
		if bal, berr := g.Balance.Fetch(g.Signer.Address()); berr == nil && int64(bal*1_000_000) < amountMicro {
			g.setLastFetchError("insufficient_funds", amountMicro, false, target, "insufficient_funds")
			LogPayment(g.Logger, amountMicro, target, policyOutcome("insufficient_funds"), overrideAmountMicro > 0)
			return nil, &PolicyError{Code: "insufficient_funds", AmountMicro: amountMicro, CanOverride: false}
		}
	}

	// --- Signing and retry (extracted to fetch_sign.go) ---
	return g.signAndRetry(ctx, method, target, body, headers, pr, req, overrideAmountMicro, key, amountMicro, amountErr)
}

// evaluateSeller checks seller trust (013.2, TOFU) and returns a PolicyError if
// the seller is unknown or the domain cap is exceeded. Nil means OK.
func (g *Gateway) evaluateSeller(target string, approveSeller bool, pol *policy.Policy, req *x402.PaymentRequirements, overrideAmountMicro int64) error {
	if g.Sellers == nil {
		return nil
	}
	domain := normSellerDomain(target)
	if domain == "" {
		g.setLastFetchError("unknown_seller", 0, true, target, "unknown_seller")
		return &PolicyError{Code: "unknown_seller", AmountMicro: 0, CanOverride: true}
	}
	if approveSeller {
		if err := g.Sellers.Land(domain); err != nil && g.Logger != nil {
			// deliberate: the payment itself is user-approved, so a persist
			// failure only warns — the next payment will ask again (fail-closed).
			g.Logger.Warn("sellers land", "domain", domain, "err", err)
		}
		return nil
	}
	known, err := g.Sellers.Known(domain)
	if err != nil {
		g.setLastFetchError("upstream_error", 0, false, target, err.Error())
		return err
	}
	amountMicro, _ := strconv.ParseInt(req.Amount, 10, 64)
	if !known {
		g.setLastFetchError("unknown_seller", amountMicro, true, target, "unknown_seller")
		return &PolicyError{Code: "unknown_seller", AmountMicro: amountMicro, CanOverride: true}
	}
	daySpend, err := g.Sellers.Today(domain)
	if err != nil {
		g.setLastFetchError("upstream_error", 0, false, target, err.Error())
		return err
	}
	subCap := pol.DomainSubCapMicro()
	if subCap > 0 && (daySpend < 0 || amountMicro > subCap-daySpend) {
		g.setLastFetchError("domain_cap_exceeded", amountMicro, true, target, "domain_cap_exceeded")
		return &PolicyError{Code: "domain_cap_exceeded", AmountMicro: amountMicro, CanOverride: true}
	}
	return nil
}
