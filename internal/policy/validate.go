package policy

import (
	"fmt"
	"strings"

	"gateway/internal/agentlabel"
	"gateway/internal/chains"
)

func (p *Policy) validate() error {
	if p.DailyCapMicro < 0 {
		return fmt.Errorf("daily cap must not be negative")
	}
	if p.DomainSubCapPercent < 0 || p.DomainSubCapPercent > 100 {
		return fmt.Errorf("domain_sub_cap_percent must be 0-100")
	}
	if len(p.AllowedNetworks) == 0 || len(p.PinnedAssets) == 0 {
		return fmt.Errorf("no allowed networks/assets")
	}
	if _, _, err := narrowPins(p.AllowedNetworks, p.PinnedAssets); err != nil {
		return err
	}
	if p.AgentDailyCapMicro < 0 {
		return fmt.Errorf("agent_daily_cap_micro_usdc must not be negative")
	}
	if len(p.AgentCapsMicro) > MaxAgentCaps {
		return fmt.Errorf("agent_caps_micro_usdc: at most %d entries", MaxAgentCaps)
	}
	for label, cap := range p.AgentCapsMicro {
		if !agentlabel.Valid(label) {
			return fmt.Errorf("agent_caps_micro_usdc: invalid label %q", label)
		}
		if cap < 0 {
			return fmt.Errorf("agent_caps_micro_usdc[%q] must not be negative", label)
		}
	}
	return nil
}

// Validate is the exported form of validate for socket handlers (55.5).
func (p *Policy) Validate() error { return p.validate() }

// narrowPins enforces chains SSOT: file may only narrow SupportedCAIP2s /
// USDCContract — never extend (THREAT-MODEL T1 / 45.6). Rejects unknown
// networks, wrong pins, or pins not listed in allowed_networks.
func narrowPins(nets []string, pins map[string]string) ([]string, map[string]string, error) {
	outNets := make([]string, 0, len(nets))
	outPins := make(map[string]string, len(nets))
	seen := make(map[string]bool, len(nets))
	for _, n := range nets {
		n = strings.TrimSpace(n)
		if n == "" {
			return nil, nil, fmt.Errorf("empty network in allowed_networks")
		}
		if !chains.IsSupported(n) {
			return nil, nil, fmt.Errorf("unsupported network %q (policy may only narrow chains SSOT)", n)
		}
		if seen[n] {
			continue
		}
		want := chains.USDCContract(n)
		got, ok := pins[n]
		if !ok {
			return nil, nil, fmt.Errorf("missing pinned_assets for %s", n)
		}
		if !strings.EqualFold(strings.TrimSpace(got), want) {
			return nil, nil, fmt.Errorf("pinned asset for %s must be code USDC %s", n, want)
		}
		seen[n] = true
		outNets = append(outNets, n)
		outPins[n] = want
	}
	for net := range pins {
		if !seen[net] {
			return nil, nil, fmt.Errorf("pinned_assets network %q not in allowed_networks or unsupported", net)
		}
	}
	if len(outNets) == 0 {
		return nil, nil, fmt.Errorf("no allowed networks/assets")
	}
	return outNets, outPins, nil
}
