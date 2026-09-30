// fetch.go — doFetch orchestration + evaluateSeller.
// The signing/retry section is in fetch_sign.go, first-request parsing in fetch_parse.go.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"gateway/internal/budget"
	"gateway/internal/policy"
	"gateway/internal/x402"
)

// doFetch is the shared implementation for Fetch and FetchWithOverride.
// If overrideAmountMicro > 0, the daily-cap test is skipped (the user already
// approved this over-budget payment); the amount is still reserved.
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
	// Unsettled flows release the claim; paid flows keep markSigned.
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
		res, rerr := toResult(first)
		if rerr != nil {
			g.setLastFetchError("content_too_large", 0, false, target, rerr.Error())
			return nil, fmt.Errorf("%w: %v", ErrContentTooLarge, rerr)
		}
		return res, nil
	}

	// --- Static policy + seller trust (TOFU); budget is Authorize below ---
	pol := g.CurrentPolicy()
	var perr error
	if overrideAmountMicro > 0 {
		perr = pol.CheckOverride(req)
	} else {
		perr = pol.CheckStatic(req)
	}
	if serr := g.evaluateSeller(target, approveSeller, req); serr != nil {
		if perr == nil {
			perr = serr
		}
	}
	if perr != nil {
		amountMicro, _ := strconv.ParseInt(req.Amount, 10, 64)
		g.recordBlock(perr.Error(), req.Amount, target)
		g.setLastFetchError(perr.Error(), amountMicro, false, target, perr.Error())
		audited = true
		LogPayment(g.Logger, amountMicro, target, policyOutcome(perr.Error()), overrideAmountMicro > 0)
		if _, isPolicy := perr.(*PolicyError); isPolicy {
			return nil, perr
		}
		return nil, &PolicyError{Code: perr.Error()}
	}

	amountMicro, amountErr := strconv.ParseInt(req.Amount, 10, 64)
	auditAmount = amountMicro
	if overrideAmountMicro > 0 && amountMicro > overrideAmountMicro {
		g.setLastFetchError("price_changed", amountMicro, true, target, "price_changed")
		return nil, &PolicyError{Code: "price_changed", AmountMicro: amountMicro, CanOverride: true}
	}

	// --- Funds pre-check (advisory; settle is the fail-closed gate) ---
	if g.Balance != nil && amountMicro > 0 {
		if bal, berr := g.Balance.Fetch(g.Signer.Address()); berr == nil && int64(bal*1_000_000) < amountMicro {
			g.setLastFetchError("insufficient_funds", amountMicro, false, target, "insufficient_funds")
			LogPayment(g.Logger, amountMicro, target, policyOutcome("insufficient_funds"), overrideAmountMicro > 0)
			return nil, &PolicyError{Code: "insufficient_funds", AmountMicro: amountMicro, CanOverride: false}
		}
	}

	// --- Atomic budget reservation (charge before sign) ---
	token, aerr := g.authorizePayment(amountMicro, amountErr, target, pol, overrideAmountMicro, approveSeller)
	if aerr != nil {
		audited = true
		return nil, aerr
	}

	return g.signAndRetry(ctx, method, target, body, headers, pr, req, overrideAmountMicro, key, amountMicro, amountErr, token)
}

// authorizePayment reserves amountMicro against the daily cap and domain
// sub-cap. Override skips the daily-cap ceiling (cap=∞) but still reserves;
// approveSeller disables the domain sub-cap (explicit TOFU approval).
func (g *Gateway) authorizePayment(amountMicro int64, amountErr error, target string, pol *policy.Policy, overrideAmountMicro int64, approveSeller bool) (string, error) {
	if amountErr != nil || amountMicro <= 0 {
		return "", nil
	}
	if g.Budget == nil {
		g.setLastFetchError("upstream_error", amountMicro, false, target, "budget authority missing")
		return "", fmt.Errorf("%w: budget authority missing", ErrUpstream)
	}
	capMicro := pol.DailyCapMicro
	if overrideAmountMicro > 0 {
		capMicro = math.MaxInt64
	}
	subcap := pol.DomainSubCapMicro()
	domain := normSellerDomain(target)
	if approveSeller {
		subcap = 0
	} else if subcap > 0 && g.Sellers != nil && domain != "" {
		// Sellers holds sequential domain spend; Reserved covers concurrency.
		daySpend, err := g.Sellers.Today(domain)
		if err != nil {
			g.setLastFetchError("upstream_error", amountMicro, false, target, err.Error())
			return "", err
		}
		if daySpend < 0 {
			daySpend = 0
		}
		rem := subcap - daySpend
		if rem <= 0 {
			g.recordBlock("budget_exceeded", strconv.FormatInt(amountMicro, 10), target)
			g.setLastFetchError("budget_exceeded", amountMicro, true, target, "budget_exceeded")
			LogPayment(g.Logger, amountMicro, target, "failed:budget_exceeded", overrideAmountMicro > 0)
			return "", &PolicyError{Code: "budget_exceeded", AmountMicro: amountMicro, CanOverride: true}
		}
		subcap = rem
	}
	token, err := g.Budget.Authorize(amountMicro, capMicro, subcap, domain)
	if err != nil {
		if errors.Is(err, budget.ErrBudget) {
			g.recordBlock("budget_exceeded", strconv.FormatInt(amountMicro, 10), target)
			g.setLastFetchError("budget_exceeded", amountMicro, true, target, "budget_exceeded")
			LogPayment(g.Logger, amountMicro, target, "failed:budget_exceeded", overrideAmountMicro > 0)
			return "", &PolicyError{Code: "budget_exceeded", AmountMicro: amountMicro, CanOverride: true}
		}
		g.setLastFetchError("upstream_error", amountMicro, false, target, err.Error())
		return "", fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	return token, nil
}

// evaluateSeller checks seller trust (013.2, TOFU). Domain sub-cap lives in
// authorizePayment. Nil means OK.
func (g *Gateway) evaluateSeller(target string, approveSeller bool, req *x402.PaymentRequirements) error {
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
	return nil
}
