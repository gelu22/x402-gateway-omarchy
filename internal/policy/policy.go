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
		return Default(), fmt.Errorf("policy: corrupt %s, using defaults", path)
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
		p.AllowedNetworks = pf.AllowedNetworks
		p.PinnedAssets = pf.PinnedAssets
	}
	if err := p.validate(); err != nil {
		return Default(), fmt.Errorf("policy: invalid %s (%v), using defaults", path, err)
	}
	if p.BuilderCode != "" && !x402.ValidBuilderCode(p.BuilderCode) {
		p.BuilderCode = ""
		return p, fmt.Errorf("policy: invalid builder_code in %s, attribution disabled", path)
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
	return nil
}

// MustLoad loads the policy, logging a warning and falling back to defaults.
func MustLoad(stateDir string, logger interface{ Warn(string, ...any) }) *Policy {
	p, err := Load(stateDir)
	if err != nil && logger != nil {
		logger.Warn("policy: " + err.Error())
	}
	return p
}
