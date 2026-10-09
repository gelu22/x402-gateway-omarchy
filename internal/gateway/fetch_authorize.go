// fetch_authorize.go — authorizePayment + evaluateSeller (split from fetch.go for LOC).
package gateway

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"gateway/internal/budget"
	"gateway/internal/policy"
	"gateway/internal/x402"
)

// authorizePayment: daily/domain/agent caps; override lifts daily+agent,
// approveSeller lifts sub-cap only (not agent).
func (g *Gateway) authorizePayment(amountMicro int64, amountErr error, target string, pol *policy.Policy, overrideAmountMicro int64, approveSeller bool, agent string) (string, error) {
	if amountErr != nil || amountMicro <= 0 {
		return "", nil
	}
	if g.Budget == nil {
		g.setLastFetchError("upstream_error", amountMicro, false, target, "budget authority missing")
		return "", fmt.Errorf("%w: budget authority missing", ErrUpstream)
	}
	capMicro := pol.DailyCapMicro
	agentCap := pol.AgentCapMicro(agent)
	// An explicit 0 for a label means "this agent does not auto-pay" (policy.go
	// field comment). AgentCapMicro cannot express that on its own — it returns 0
	// for both "no entry (feature off)" and "explicit 0" — so a bare 0 would slip
	// past the agent check in Authority and hand the agent the whole global budget.
	agentNoAutoPay := pol.AgentCapExplicit(agent) && agentCap == 0
	if overrideAmountMicro > 0 {
		capMicro = math.MaxInt64
		agentCap = 0 // owner override lifts the agent limit too
		agentNoAutoPay = false
	}
	subcap := pol.DomainSubCapMicro()
	domain := normSellerDomain(target)
	if approveSeller {
		subcap = 0
	} else if subcap > 0 && domain == "" {
		g.setLastFetchError("unknown_seller", 0, true, target, "unknown_seller")
		return "", &PolicyError{Code: "unknown_seller", AmountMicro: 0, CanOverride: true}
	}
	if agentNoAutoPay {
		g.recordBlock("agent_cap_exceeded", strconv.FormatInt(amountMicro, 10), target)
		g.setLastFetchError("agent_cap_exceeded", amountMicro, true, target, "agent_cap_exceeded")
		LogPayment(g.Logger, PaymentLine{
			AmountMicro: amountMicro, Target: target,
			Outcome: "failed:agent_cap_exceeded", Agent: agent,
			Override: overrideAmountMicro > 0,
		})
		return "", &PolicyError{Code: "agent_cap_exceeded", AmountMicro: amountMicro, CanOverride: true}
	}
	// The per-domain cap is passed whole: the authority decides it from its own
	// committed plus in-flight totals, in one transaction. Subtracting a balance
	// read from the Sellers registry here was check-then-act — a payment settled
	// in between was in neither store's view of the domain, so two ordinary
	// concurrent requests could exceed the cap (47.1).
	token, err := g.Budget.Authorize(
		budget.Hold{AmountMicro: amountMicro, Domain: domain, Agent: agent},
		budget.Caps{DailyMicro: capMicro, DomainMicro: subcap, AgentMicro: agentCap},
	)
	if err != nil {
		if errors.Is(err, budget.ErrSubCap) {
			g.recordBlock("domain_cap_exceeded", strconv.FormatInt(amountMicro, 10), target)
			g.setLastFetchError("domain_cap_exceeded", amountMicro, true, target, err.Error())
			LogPayment(g.Logger, PaymentLine{
				AmountMicro: amountMicro, Target: target,
				Outcome: "failed:domain_cap_exceeded", Agent: agent,
				Override: overrideAmountMicro > 0,
			})
			return "", &PolicyError{Code: "domain_cap_exceeded", AmountMicro: amountMicro, CanOverride: true}
		}
		if errors.Is(err, budget.ErrBudget) {
			g.recordBlock("budget_exceeded", strconv.FormatInt(amountMicro, 10), target)
			g.setLastFetchError("budget_exceeded", amountMicro, true, target, "budget_exceeded")
			LogPayment(g.Logger, PaymentLine{
				AmountMicro: amountMicro, Target: target,
				Outcome: "failed:budget_exceeded", Agent: agent,
				Override: overrideAmountMicro > 0,
			})
			return "", &PolicyError{Code: "budget_exceeded", AmountMicro: amountMicro, CanOverride: true}
		}
		if errors.Is(err, budget.ErrAgentCap) {
			g.recordBlock("agent_cap_exceeded", strconv.FormatInt(amountMicro, 10), target)
			g.setLastFetchError("agent_cap_exceeded", amountMicro, true, target, "agent_cap_exceeded")
			LogPayment(g.Logger, PaymentLine{
				AmountMicro: amountMicro, Target: target,
				Outcome: "failed:agent_cap_exceeded", Agent: agent,
				Override: overrideAmountMicro > 0,
			})
			return "", &PolicyError{Code: "agent_cap_exceeded", AmountMicro: amountMicro, CanOverride: true}
		}
		g.setLastFetchError("upstream_error", amountMicro, false, target, err.Error())
		return "", fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	return token, nil
}

// evaluateSeller: seller trust (013.2 TOFU). Nil = OK. Domain sub-cap in authorizePayment.
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
		if err := g.Sellers.Land(domain); err != nil {
			if g.Logger != nil {
				g.Logger.Error("sellers land", "domain", domain, "err", err)
			}
			g.setLastFetchError("upstream_error", 0, false, target, err.Error())
			return &PolicyError{Code: "upstream_error", CanOverride: false}
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
