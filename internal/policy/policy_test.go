package policy

import (
	"os"
	"path/filepath"
	"testing"

	"gateway/internal/x402"
)

const usdc = "0x036CbD53842c5426634e7929541eC2318f3dCF7e"

func validReq(amount string) *x402.PaymentRequirements {
	return &x402.PaymentRequirements{
		Scheme: "exact", Network: "eip155:84532",
		Asset:  usdc,
		Amount: amount, PayTo: "0x19c1d70Df1F5179CfD015A88Acc7371E203B092C",
		MaxTimeoutSeconds: 600,
	}
}

func TestCheckWithinDailyCap(t *testing.T) {
	p := Default()
	if err := p.Check(validReq("250001"), 0); err != nil {
		t.Fatalf("within cap should pass, got %v", err)
	}
	if err := p.Check(validReq("1000000"), 4_000_000); err != nil {
		t.Fatalf("spend+amount == cap should pass, got %v", err)
	}
}

func TestCheckOverDailyCap(t *testing.T) {
	p := Default() // $5 cap
	if err := p.Check(validReq("5000001"), 0); err == nil || err.Error() != "budget_exceeded" {
		t.Fatalf("want budget_exceeded, got %v", err)
	}
	if err := p.Check(validReq("1000000"), 4_500_001); err == nil || err.Error() != "budget_exceeded" {
		t.Fatalf("spend+amount > cap: want budget_exceeded, got %v", err)
	}
}

func TestZeroCapAlwaysAsks(t *testing.T) {
	p := Default()
	p.DailyCapMicro = 0
	if err := p.Check(validReq("1"), 0); err == nil || err.Error() != "budget_exceeded" {
		t.Fatalf("cap 0 must always ask, got %v", err)
	}
	// 0 is a valid policy value (not rejected by Save/validate).
	if err := p.Save(t.TempDir()); err != nil {
		t.Fatalf("cap 0 must be saveable, got %v", err)
	}
}

func TestCheckOverrideSkipsDailyBudget(t *testing.T) {
	p := Default()
	if err := p.Check(validReq("10000000"), 0); err == nil || err.Error() != "budget_exceeded" {
		t.Fatalf("over cap must deny in Check, got %v", err)
	}
	if err := p.CheckOverride(validReq("10000000")); err != nil {
		t.Fatalf("CheckOverride must skip budget, got %v", err)
	}
	// Override still enforces network/asset/amount hygiene.
	bad := validReq("10000000")
	bad.Asset = "0xDeadBeef00000000000000000000000000000001"
	if err := p.CheckOverride(bad); err == nil || err.Error() != "network_denied" {
		t.Fatalf("override must keep network_denied, got %v", err)
	}
	if err := p.CheckOverride(validReq("-5")); err == nil || err.Error() != "invalid_amount" {
		t.Fatalf("override must keep invalid_amount guard, got %v", err)
	}
}

func TestCheckUnknownNetworkAndScheme(t *testing.T) {
	p := Default()
	req := validReq("10000")
	req.Network = "eip155:1"
	if err := p.Check(req, 0); err == nil {
		t.Fatal("unknown network must be denied")
	}
	req2 := validReq("10000")
	req2.Scheme = "upto"
	if err := p.Check(req2, 0); err == nil {
		t.Fatal("non-exact scheme must be denied")
	}
}

func TestCheckNonCanonicalAmountsDenied(t *testing.T) {
	p := Default()
	for _, amount := range []string{"1e9", "-5", "0", "10 USD", ""} {
		if err := p.Check(validReq(amount), 0); err == nil || err.Error() != "invalid_amount" {
			t.Fatalf("amount %q: want invalid_amount, got %v", amount, err)
		}
	}
}

func TestDomainSubCapMicro(t *testing.T) {
	p := Default() // cap $5, 20%
	if got := p.DomainSubCapMicro(); got != 1_000_000 {
		t.Fatalf("20%% of $5 = $1, got %d", got)
	}
	p.DomainSubCapPercent = 0
	if got := p.DomainSubCapMicro(); got != 0 {
		t.Fatalf("0 percent = disabled → 0, got %d", got)
	}
	p.DomainSubCapPercent = 100
	if got := p.DomainSubCapMicro(); got != p.DailyCapMicro {
		t.Fatalf("100%% = cap, got %d", got)
	}
}

func TestLoadLegacyBucketsResetToDefault(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"daily_cap_ppv_micro_usdc": 3000000, "daily_cap_ppc_micro_usdc": 1000000,
	            "ppc_threshold_micro_usdc": 10000, "per_request_cap_micro_usdc": 250000,
	            "allowed_networks": ["eip155:84532"], "pinned_assets": {"eip155:84532": "` + usdc + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.DailyCapMicro != DefaultDailyCapMicro || p.DomainSubCapPercent != DefaultDomainSubCapPercent {
		t.Fatalf("legacy file must reset to defaults, got %+v", p)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := Default()
	p.DailyCapMicro = 0 // always ask: must survive (explicit 0, not defaulted)
	p.DomainSubCapPercent = 35
	if err := p.Save(dir); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.DailyCapMicro != 0 || got.DomainSubCapPercent != 35 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	info, _ := os.Stat(filepath.Join(dir, "policy.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("policy.json perms %v, want 0600", info.Mode().Perm())
	}
}

func TestLoadMissingFileUsesDefault(t *testing.T) {
	p, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("missing file must Default, got %v", err)
	}
	if p.DailyCapMicro != DefaultDailyCapMicro {
		t.Fatalf("want DefaultDailyCapMicro, got %d", p.DailyCapMicro)
	}
}

func TestLoadCorruptFailsClosed(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "policy.json"), []byte("{"), 0o600)
	p, err := Load(dir)
	if err == nil {
		t.Fatal("corrupt JSON must err")
	}
	if p != nil {
		t.Fatalf("corrupt must not return a policy (got %+v)", p)
	}
}

func TestLoadEmptyFailsClosed(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "policy.json"), []byte(""), 0o600)
	p, err := Load(dir)
	if err == nil || p != nil {
		t.Fatalf("empty file must fail-closed, got p=%v err=%v", p, err)
	}
}

func writePolicy(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func basePolicyJSON(extra string) string {
	return `{"daily_cap_micro_usdc": 1000000, "domain_sub_cap_percent": 20,` + extra + `
            "allowed_networks": ["eip155:84532"], "pinned_assets": {"eip155:84532": "` + usdc + `"}}`
}

func TestLoadBuilderCodePersistsWhenValid(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, basePolicyJSON(`"builder_code": "bc_lgtoqsts",`))
	p, err := Load(dir)
	if err != nil {
		t.Fatalf("valid builder code must load clean, got %v", err)
	}
	if p.BuilderCode != "bc_lgtoqsts" {
		t.Fatalf("want bc_lgtoqsts, got %q", p.BuilderCode)
	}
	if p.DailyCapMicro != 1_000_000 {
		t.Fatalf("cap must survive alongside builder code, got %+v", p)
	}
}

func TestLoadBuilderCodeStrippedWhenInvalid(t *testing.T) {
	dir := t.TempDir()
	writePolicy(t, dir, basePolicyJSON(`"builder_code": "NOT VALID!!",`))
	p, err := Load(dir)
	if err != nil {
		t.Fatalf("invalid builder_code must not abort Load, got %v", err)
	}
	if p.BuilderCode != "" {
		t.Fatalf("invalid code must be stripped, got %q", p.BuilderCode)
	}
	if p.DailyCapMicro != 1_000_000 {
		t.Fatalf("cap must survive a bad builder code, got %+v", p)
	}
}

// Negative cap / out-of-range sub-cap refuse Load — never Default success (45.2).
func TestTamperedValuesFailClosed(t *testing.T) {
	dir := t.TempDir()
	raw := `{"daily_cap_micro_usdc":-1,"domain_sub_cap_percent":150,"allowed_networks":["eip155:84532"],"pinned_assets":{"eip155:84532":"` + usdc + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err == nil {
		t.Fatal("want error for invalid values, got nil")
	}
	if p != nil {
		t.Fatalf("invalid ranges must not return policy (no Default widen), got %+v", p)
	}
}

// Tampered spend near MaxInt64 must not wrap around the budget check (016.4).
func TestCheckOverflowNoBypass(t *testing.T) {
	p := Default()
	huge := int64(9223372036854775800) // MaxInt64 - 7
	if err := p.Check(validReq("8000"), huge); err == nil || err.Error() != "budget_exceeded" {
		t.Fatalf("want budget_exceeded, got %v", err)
	}
}

// 45.6: file may narrow chains SSOT, never extend (Load reject).
func TestLoadRejectsExpandedNetwork(t *testing.T) {
	dir := t.TempDir()
	raw := `{"daily_cap_micro_usdc":100,"domain_sub_cap_percent":20,"allowed_networks":["eip155:1"],"pinned_assets":{"eip155:1":"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"}}`
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err == nil || p != nil {
		t.Fatalf("expanded network must fail Load, got p=%v err=%v", p, err)
	}
}

func TestLoadRejectsWrongUSDCPin(t *testing.T) {
	dir := t.TempDir()
	raw := `{"daily_cap_micro_usdc":100,"domain_sub_cap_percent":20,"allowed_networks":["eip155:84532"],"pinned_assets":{"eip155:84532":"0xDeadBeef00000000000000000000000000000001"}}`
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err == nil || p != nil {
		t.Fatalf("wrong USDC pin must fail Load, got p=%v err=%v", p, err)
	}
}

func TestLoadRejectsMixedSupportedAndUnsupported(t *testing.T) {
	dir := t.TempDir()
	raw := `{"daily_cap_micro_usdc":100,"domain_sub_cap_percent":20,"allowed_networks":["eip155:84532","eip155:1"],"pinned_assets":{"eip155:84532":"` + usdc + `","eip155:1":"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"}}`
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err == nil || p != nil {
		t.Fatalf("mixed list must fail closed (no half-apply), got p=%v err=%v", p, err)
	}
}

func TestLoadNarrowsToOneSupportedNetwork(t *testing.T) {
	dir := t.TempDir()
	const baseUSDC = "0x833589fCD6eDb6E08f4c7C32D4f71b54bdA02913"
	raw := `{"daily_cap_micro_usdc":100,"domain_sub_cap_percent":20,"allowed_networks":["eip155:8453"],"pinned_assets":{"eip155:8453":"` + baseUSDC + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatalf("narrow to Base must Load, got %v", err)
	}
	if len(p.AllowedNetworks) != 1 || p.AllowedNetworks[0] != "eip155:8453" {
		t.Fatalf("want only eip155:8453, got %v", p.AllowedNetworks)
	}
	sepolia := validReq("100")
	if err := p.CheckStatic(sepolia); err == nil || err.Error() != "network_denied" {
		t.Fatalf("Sepolia req after Base-only policy: want network_denied, got %v", err)
	}
	base := validReq("100")
	base.Network = "eip155:8453"
	base.Asset = baseUSDC
	if err := p.CheckStatic(base); err != nil {
		t.Fatalf("Base+official USDC must pass CheckStatic, got %v", err)
	}
	evil := validReq("100")
	evil.Network = "eip155:1"
	evil.Asset = "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"
	if err := p.CheckStatic(evil); err == nil {
		t.Fatal("CheckStatic must not allow eip155:1 on loaded policy")
	}
}

func TestSaveRejectsExpandedPolicy(t *testing.T) {
	p := Default()
	p.AllowedNetworks = []string{"eip155:1"}
	p.PinnedAssets = map[string]string{"eip155:1": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"}
	if err := p.Save(t.TempDir()); err == nil {
		t.Fatal("Save must reject expanded networks")
	}
}

func TestAgentCapMicroMissingFields(t *testing.T) {
	dir := t.TempDir()
	raw := `{"daily_cap_micro_usdc":5000000,"domain_sub_cap_percent":20,` +
		`"allowed_networks":["eip155:84532"],"pinned_assets":{"eip155:84532":"` + usdc + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.AgentCapMicro("x") != 0 || p.AgentCapMicro("") != 0 {
		t.Fatalf("missing agent fields must yield 0, got default=%d x=%d", p.AgentDailyCapMicro, p.AgentCapMicro("x"))
	}
}

func TestAgentCapMicroDefaultAndOverride(t *testing.T) {
	p := Default()
	p.AgentDailyCapMicro = 1_000_000
	if p.AgentCapMicro("codex") != 1_000_000 || p.AgentCapMicro("") != 1_000_000 {
		t.Fatalf("default must apply to every label incl empty, got %d / %d", p.AgentCapMicro("codex"), p.AgentCapMicro(""))
	}
	p.AgentCapsMicro = map[string]int64{"codex": 200_000}
	if p.AgentCapMicro("codex") != 200_000 {
		t.Fatalf("override want 200000, got %d", p.AgentCapMicro("codex"))
	}
	if p.AgentCapMicro("claude") != 1_000_000 {
		t.Fatalf("non-override want default, got %d", p.AgentCapMicro("claude"))
	}
}

func TestAgentCapMicroZeroMeanings(t *testing.T) {
	p := Default()
	// Default 0 = feature off → AgentCapMicro is 0 for all (no map entry).
	if p.AgentCapMicro("codex") != 0 {
		t.Fatalf("feature off want 0, got %d", p.AgentCapMicro("codex"))
	}
	// Explicit override 0 with feature on = that agent does not auto-pay.
	p.AgentDailyCapMicro = 1_000_000
	p.AgentCapsMicro = map[string]int64{"codex": 0}
	if p.AgentCapMicro("codex") != 0 {
		t.Fatalf("override 0 want 0, got %d", p.AgentCapMicro("codex"))
	}
	if p.AgentCapMicro("other") != 1_000_000 {
		t.Fatalf("other want default, got %d", p.AgentCapMicro("other"))
	}
}

func TestLoadAgentCapsRejectsBad(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base := `"allowed_networks":["eip155:84532"],"pinned_assets":{"eip155:84532":"` + usdc + `"}`
	write(`{"agent_daily_cap_micro_usdc":-1,` + base + `}`)
	if _, err := Load(dir); err == nil {
		t.Fatal("negative agent_daily_cap must fail")
	}
	write(`{"agent_caps_micro_usdc":{"Bad":1000},` + base + `}`)
	if _, err := Load(dir); err == nil {
		t.Fatal("uppercase label must fail")
	}
	write(`{"agent_caps_micro_usdc":{"codex":-5},` + base + `}`)
	if _, err := Load(dir); err == nil {
		t.Fatal("negative override must fail")
	}
}

func TestSaveLoadAgentCapsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := Default()
	p.AgentDailyCapMicro = 1_000_000
	p.AgentCapsMicro = map[string]int64{"codex": 200_000, "claude": 0}
	if err := p.Save(dir); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.AgentDailyCapMicro != 1_000_000 {
		t.Fatalf("default round-trip: got %d", got.AgentDailyCapMicro)
	}
	if got.AgentCapMicro("codex") != 200_000 || got.AgentCapMicro("claude") != 0 {
		t.Fatalf("map round-trip: %+v", got.AgentCapsMicro)
	}
	if got.AgentCapMicro("") != 1_000_000 {
		t.Fatalf("empty label want default, got %d", got.AgentCapMicro(""))
	}
}
