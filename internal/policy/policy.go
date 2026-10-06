// Package policy enforces client-side spend limits (THREAT-MODEL T1).
// Split: policy.go (types, Load, Save, Default, AgentCapMicro),
// validate.go (validate, narrowPins), check.go (Check).
package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gateway/internal/chains"
	"gateway/internal/x402"
)

const (
	DefaultDailyCapMicro       = 5_000_000
	DefaultDomainSubCapPercent = 20
	MaxAgentCaps               = 64
	filePerms                  = 0o600
)

type Policy struct {
	DailyCapMicro       int64             `json:"daily_cap_micro_usdc"`
	DomainSubCapPercent int               `json:"domain_sub_cap_percent"`
	BuilderCode         string            `json:"builder_code,omitempty"`
	AllowedNetworks     []string          `json:"allowed_networks"`
	PinnedAssets        map[string]string `json:"pinned_assets"`
	// AgentDailyCapMicro is the default per-agent daily limit (micro-USDC).
	// 0 = per-agent caps disabled (today's behavior). Callers (55.4) must treat
	// a missing map entry + 0 default as "no agent-cap check", not "always ask".
	AgentDailyCapMicro int64 `json:"agent_daily_cap_micro_usdc"`
	// AgentCapsMicro overrides AgentDailyCapMicro per non-empty label.
	// An explicit 0 for a label means that agent does not auto-pay (like
	// daily_cap 0 = always ask) — distinct from a 0 default (feature off).
	AgentCapsMicro map[string]int64 `json:"agent_caps_micro_usdc,omitempty"`
}

func Default() *Policy {
	pinned := make(map[string]string)
	for _, caip2 := range chains.SupportedCAIP2s() {
		pinned[caip2] = chains.USDCContract(caip2)
	}
	return &Policy{
		DailyCapMicro:       DefaultDailyCapMicro,
		DomainSubCapPercent: DefaultDomainSubCapPercent,
		AllowedNetworks:     chains.SupportedCAIP2s(),
		PinnedAssets:        pinned,
		AgentDailyCapMicro:  0,
	}
}

type policyFile struct {
	DailyCapMicro       *int64            `json:"daily_cap_micro_usdc"`
	DomainSubCapPercent *int              `json:"domain_sub_cap_percent"`
	BuilderCode         string            `json:"builder_code"`
	AllowedNetworks     []string          `json:"allowed_networks"`
	PinnedAssets        map[string]string `json:"pinned_assets"`
	AgentDailyCapMicro  *int64            `json:"agent_daily_cap_micro_usdc"`
	AgentCapsMicro      map[string]int64  `json:"agent_caps_micro_usdc"`
}

func Load(stateDir string) (*Policy, error) {
	path := filepath.Join(stateDir, "policy.json")
	raw, err := os.ReadFile(path) // #nosec G304 -- path is stateDir/policy.json (same-user trust); no remote input
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("policy: read %s: %w", path, err)
	}
	var pf policyFile
	if err := json.Unmarshal(raw, &pf); err != nil {
		return nil, fmt.Errorf("policy: corrupt %s: %w", path, err)
	}
	p := Default()
	if pf.DailyCapMicro != nil {
		p.DailyCapMicro = *pf.DailyCapMicro
	}
	if pf.DomainSubCapPercent != nil {
		p.DomainSubCapPercent = *pf.DomainSubCapPercent
	}
	if sf := strings.TrimSpace(pf.BuilderCode); sf != "" {
		p.BuilderCode = sf
	}
	if len(pf.AllowedNetworks) > 0 && len(pf.PinnedAssets) > 0 {
		nets, pins, err := narrowPins(pf.AllowedNetworks, pf.PinnedAssets)
		if err != nil {
			return nil, fmt.Errorf("policy: invalid %s: %w", path, err)
		}
		p.AllowedNetworks = nets
		p.PinnedAssets = pins
	}
	if pf.AgentDailyCapMicro != nil {
		p.AgentDailyCapMicro = *pf.AgentDailyCapMicro
	}
	if pf.AgentCapsMicro != nil {
		p.AgentCapsMicro = make(map[string]int64, len(pf.AgentCapsMicro))
		for k, v := range pf.AgentCapsMicro {
			p.AgentCapsMicro[k] = v
		}
	}
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("policy: invalid %s: %w", path, err)
	}
	// Invalid attribution must not abort start or widen caps — strip only.
	if p.BuilderCode != "" && !x402.ValidBuilderCode(p.BuilderCode) {
		p.BuilderCode = ""
	}
	return p, nil
}

func (p *Policy) Save(stateDir string) error {
	if err := p.validate(); err != nil {
		return fmt.Errorf("policy: validate: %w", err)
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(stateDir, "policy.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, filePerms); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AgentCapMicro returns the daily micro-USDC limit for label: map override,
// else AgentDailyCapMicro. Empty label uses the default (no "" key allowed).
// See field comments for the two meanings of 0.
func (p *Policy) AgentCapMicro(label string) int64 {
	if p == nil {
		return 0
	}
	if p.AgentCapsMicro != nil {
		if v, ok := p.AgentCapsMicro[label]; ok {
			return v
		}
	}
	return p.AgentDailyCapMicro
}
