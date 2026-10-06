// fetch.go — doFetch (authorize in fetch_authorize.go; sign in fetch_sign.go; parse in fetch_parse.go).
package gateway

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"gateway/internal/agentlabel"
)

// doFetch is the shared implementation for Fetch and FetchWithOverride.
// If overrideAmountMicro > 0, the daily-cap test is skipped (the user already
// approved this over-budget payment); the amount is still reserved.
func (g *Gateway) doFetch(ctx context.Context, method, target string, body []byte, headers map[string]string, overrideAmountMicro int64, approveSeller bool) (result *FetchResult, err error) {
	agent := agentlabel.From(ctx)
	var auditAmount int64
	audited := false
	defer func() {
		if err == nil || audited {
			return
		}
		LogPayment(g.Logger, PaymentLine{
			AmountMicro: auditAmount, Target: target,
			Outcome: "failed:" + auditErrorCode(err), Agent: agent,
			Override: overrideAmountMicro > 0,
		})
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
	first, pr, req, err := g.parse402Response(ctx, method, target, body, headers, key, agent)
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
	// A daemon permission (49.3) is a pre-approval for this exact URL up to its
	// cap: it answers the seller-trust and sub-cap question without a dialog.
	reqAmount, _ := strconv.ParseInt(req.Amount, 10, 64)
	approved := approveSeller || g.Permissions.Allows(target, reqAmount)
	if serr := g.evaluateSeller(target, approved, req); serr != nil {
		if perr == nil {
			perr = serr
		}
	}
	if perr != nil {
		amountMicro, _ := strconv.ParseInt(req.Amount, 10, 64)
		g.recordBlock(perr.Error(), req.Amount, target)
		g.setLastFetchError(perr.Error(), amountMicro, false, target, perr.Error())
		audited = true
		LogPayment(g.Logger, PaymentLine{
			AmountMicro: amountMicro, Target: target,
			Outcome: policyOutcome(perr.Error()), Agent: agent,
			Override: overrideAmountMicro > 0,
		})
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
			LogPayment(g.Logger, PaymentLine{
				AmountMicro: amountMicro, Target: target,
				Outcome: policyOutcome("insufficient_funds"), Agent: agent,
				Override: overrideAmountMicro > 0,
			})
			return nil, &PolicyError{Code: "insufficient_funds", AmountMicro: amountMicro, CanOverride: false}
		}
	}

	// --- Atomic budget reservation (charge before sign) ---
	token, aerr := g.authorizePayment(amountMicro, amountErr, target, pol, overrideAmountMicro, approved, agent)
	if aerr != nil {
		audited = true
		return nil, aerr
	}

	return g.signAndRetry(ctx, method, target, body, headers, pr, req, overrideAmountMicro, key, amountMicro, amountErr, token, agent)
}
