package gateway

import (
	"math"
	"path/filepath"
	"testing"

	"gateway/internal/policy"
)

func TestFormatMicroUSDC(t *testing.T) {
	tests := []struct {
		micro int64
		want  string
	}{
		{0, "0.00"},
		{1, "0.000001"},
		{1_000, "0.001"}, // $0.001 — the "0.00" regression
		{2_000, "0.002"}, // Node4All test price
		{6_000, "0.006"},
		{10_000, "0.01"},
		{100_000, "0.10"},
		{1_000_000, "1.00"},
		{5_000_000, "5.00"}, // default daily cap
		{1_234_567, "1.234567"},
		{-2_000, "-0.002"},
		// Whole int64 range: no unsigned conversion, no overflow reasoning.
		{math.MinInt64, "-9223372036854.775808"},
		{math.MaxInt64, "9223372036854.775807"},
	}
	for _, tt := range tests {
		if got := formatMicroUSDC(tt.micro); got != tt.want {
			t.Errorf("formatMicroUSDC(%d) = %q, want %q", tt.micro, got, tt.want)
		}
	}
}

func TestBudgetExhaustedBody(t *testing.T) {
	got := budgetExhaustedBody(20_000, 5_000_000)
	want := "Spent today 0.02 of 5.00 USDC. Agents stop buying until midnight."
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}

	subCent := budgetExhaustedBody(20_000, 1_000)
	if subCent != "Spent today 0.02 of 0.001 USDC. Agents stop buying until midnight." {
		t.Errorf("sub-cent cap body = %q", subCent)
	}

	if got := budgetExhaustedBody(20_000, 0); got != "Auto-pay is off (daily budget 0). Every payment will ask for approval." {
		t.Errorf("cap 0 body = %q", got)
	}
}

// TestCurrentPolicyHotReload pins the reload contract: PolicyPath is the file,
// so CurrentPolicy must load from its directory (a raw policy.Load(PolicyPath)
// double-joins policy.json and silently never reloads).
func TestCurrentPolicyHotReload(t *testing.T) {
	dir := t.TempDir()

	p := policy.Default()
	p.DailyCapMicro = 1_000_000
	if err := p.Save(dir); err != nil {
		t.Fatal(err)
	}

	gw := &Gateway{PolicyPath: filepath.Join(dir, "policy.json")}
	gw.SetPolicy(policy.Default()) // stale snapshot (5_000_000)

	p2 := policy.Default()
	p2.DailyCapMicro = 2_000_000
	if err := p2.Save(dir); err != nil {
		t.Fatal(err)
	}

	if got := gw.CurrentPolicy().DailyCapMicro; got != 2_000_000 {
		t.Fatalf("CurrentPolicy().DailyCapMicro = %d, want 2000000 (hot reload broken)", got)
	}
}
