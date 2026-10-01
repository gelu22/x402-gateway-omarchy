// Package policy enforces client-side spend limits (THREAT-MODEL T1).
// Split: policy.go (types, Load, Save, validate, Default), check.go (Check).
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
	filePerms                  = 0o600
)

type Policy struct {
	DailyCapMicro       int64             `json:"daily_cap_micro_usdc"`
	DomainSubCapPercent int               `json:"domain_sub_cap_percent"`
	BuilderCode         string            `json:"builder_code,omitempty"`
	AllowedNetworks     []string          `json:"allowed_networks"`
	PinnedAssets        map[string]string `json:"pinned_assets"`
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
	}
}

type policyFile struct {
	DailyCapMicro       *int64            `json:"daily_cap_micro_usdc"`
	DomainSubCapPercent *int              `json:"domain_sub_cap_percent"`
	BuilderCode         string            `json:"builder_code"`
	AllowedNetworks     []string          `json:"allowed_networks"`
	PinnedAssets        map[string]string `json:"pinned_assets"`
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
	return nil
}

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
