package gateway

import (
	"testing"

	"gateway/internal/budget"
	"gateway/internal/policy"
)

func TestStatusAgentsTwoSpentOnePolicyOnly(t *testing.T) {
	gw, _ := newSettleGateway(t)
	p := policy.Default()
	p.DomainSubCapPercent = 0
	p.AgentDailyCapMicro = 5_000_000
	p.AgentCapsMicro = map[string]int64{"claude": 1_000_000, "idle": 500_000}
	gw.SetPolicy(p)
	commitAgent(t, gw, 300_000, "ex.com", "codex")
	commitAgent(t, gw, 100_000, "ex.com", "claude")

	st, err := gw.Status("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Agents) != 3 {
		t.Fatalf("agents = %d, want 3 (codex spent, claude spent, idle policy-only); %+v", len(st.Agents), st.Agents)
	}
	by := map[string]AgentSpend{}
	for _, a := range st.Agents {
		by[a.Label] = a
	}
	if by["codex"].SpentTodayMicro != 300_000 || by["codex"].CapMicro != 5_000_000 {
		t.Fatalf("codex = %+v", by["codex"])
	}
	if by["claude"].SpentTodayMicro != 100_000 || by["claude"].CapMicro != 1_000_000 {
		t.Fatalf("claude = %+v", by["claude"])
	}
	if by["idle"].SpentTodayMicro != 0 || by["idle"].CapMicro != 500_000 {
		t.Fatalf("idle = %+v", by["idle"])
	}
	if st.Agents[0].Label != "codex" {
		t.Fatalf("sort: first = %q, want codex", st.Agents[0].Label)
	}
}

func TestStatusAgentsTruncatesTo32(t *testing.T) {
	gw, _ := newSettleGateway(t)
	p := policy.Default()
	p.DomainSubCapPercent = 0
	p.AgentDailyCapMicro = 1 << 62
	gw.SetPolicy(p)
	for i := 0; i < 40; i++ {
		label := "a" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		commitAgent(t, gw, int64(i+1)*1000, "ex.com", label)
	}
	st, err := gw.Status("test")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Agents) != 32 {
		t.Fatalf("len = %d, want 32", len(st.Agents))
	}
	if st.Agents[0].SpentTodayMicro < st.Agents[31].SpentTodayMicro {
		t.Fatal("must keep the largest spends")
	}
}

func TestStatusAgentsOmittedWithoutBudget(t *testing.T) {
	gw, _ := newSettleGateway(t)
	gw.Budget = nil
	st, err := gw.Status("test")
	if err != nil {
		t.Fatal(err)
	}
	if st.Agents != nil {
		t.Fatalf("agents must be omitted, got %+v", st.Agents)
	}
	if st.Version != "test" {
		t.Fatal("rest of status must still work")
	}
}

func commitAgent(t *testing.T, gw *Gateway, amount int64, domain, agent string) {
	t.Helper()
	tok, err := gw.Budget.Authorize(
		budget.Hold{AmountMicro: amount, Domain: domain, Agent: agent},
		budget.Caps{DailyMicro: 1 << 62},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.Budget.Commit(tok); err != nil {
		t.Fatal(err)
	}
}
