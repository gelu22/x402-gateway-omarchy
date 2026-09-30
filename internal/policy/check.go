// Policy validation: Check, CheckOverride, networkAllowed, isHexAddress.
package policy

import (
	"fmt"
	"strconv"
	"strings"

	"gateway/internal/x402"
)

// DomainSubCapMicro is the per-seller daily limit derived from cap and percent.
// 0 means sub-cap disabled.
func (p *Policy) DomainSubCapMicro() int64 {
	if p.DomainSubCapPercent <= 0 {
		return 0
	}
	return int64(float64(p.DailyCapMicro) * float64(p.DomainSubCapPercent) / 100)
}

// CheckStatic validates requirements against policy WITHOUT the daily budget
// (the budget is enforced atomically by internal/budget.Authorize).
func (p *Policy) CheckStatic(req *x402.PaymentRequirements) error {
	return p.check(req, 0, true)
}

// Check validates requirements against policy and single daily budget.
// Deprecated: use CheckStatic + budget.Authorize (42.1).
func (p *Policy) Check(req *x402.PaymentRequirements, spendMicro int64) error {
	return p.check(req, spendMicro, false)
}

// CheckOverride is Check without daily-budget test (user-approved over-budget).
func (p *Policy) CheckOverride(req *x402.PaymentRequirements) error {
	return p.check(req, 0, true)
}

func (p *Policy) check(req *x402.PaymentRequirements, spendMicro int64, skipBudget bool) error {
	if req.Scheme != "exact" || !p.networkAllowed(req.Network) {
		return fmt.Errorf("network_denied")
	}
	pinned, ok := p.PinnedAssets[req.Network]
	if !ok || !strings.EqualFold(req.Asset, pinned) {
		return fmt.Errorf("network_denied")
	}
	if strings.TrimSpace(req.PayTo) == "" || !isHexAddress(req.PayTo) {
		return fmt.Errorf("network_denied")
	}
	amount, err := strconv.ParseInt(strings.TrimSpace(req.Amount), 10, 64)
	if err != nil || amount <= 0 {
		return fmt.Errorf("invalid_amount")
	}
	if skipBudget {
		return nil
	}
	if spendMicro < 0 {
		return fmt.Errorf("budget_exceeded")
	}
	if amount > p.DailyCapMicro-spendMicro {
		return fmt.Errorf("budget_exceeded")
	}
	return nil
}

func (p *Policy) networkAllowed(network string) bool {
	for _, n := range p.AllowedNetworks {
		if n == network {
			return true
		}
	}
	return false
}

func isHexAddress(s string) bool {
	if !strings.HasPrefix(s, "0x") || len(s) != 42 {
		return false
	}
	for _, c := range s[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
