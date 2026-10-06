package budget

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func assertLedgersEqual(t *testing.T, a *Authority, domain, agent string, want int64) {
	t.Helper()
	today, err := a.Today()
	if err != nil {
		t.Fatalf("Today: %v", err)
	}
	dom, err := a.DomainTotal(domain)
	if err != nil {
		t.Fatalf("DomainTotal: %v", err)
	}
	ag, err := a.AgentTotal(agent)
	if err != nil {
		t.Fatalf("AgentTotal: %v", err)
	}
	if today != want || dom != want || ag != want {
		t.Fatalf("Today=%d DomainTotal=%d AgentTotal=%d, want all %d", today, dom, ag, want)
	}
}

func TestEveryPromotionMovesEveryLedger(t *testing.T) {
	const amount = int64(100_000)
	const domain = "seller.example"
	const agent = "codex"

	t.Run("Commit", func(t *testing.T) {
		a, _ := newTestAuthority(t)
		tok, err := a.Authorize(Hold{AmountMicro: amount, Domain: domain, Agent: agent}, Caps{DailyMicro: 5_000_000})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Commit(tok); err != nil {
			t.Fatal(err)
		}
		assertLedgersEqual(t, a, domain, agent, amount)
	})

	t.Run("ReleaseAfterMarkSigned", func(t *testing.T) {
		a, _ := newTestAuthority(t)
		tok, err := a.Authorize(Hold{AmountMicro: amount, Domain: domain, Agent: agent}, Caps{DailyMicro: 5_000_000})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.MarkSigned(tok); err != nil {
			t.Fatal(err)
		}
		if err := a.Release(tok); err != nil {
			t.Fatal(err)
		}
		assertLedgersEqual(t, a, domain, agent, amount)
	})

	t.Run("TTLSigned", func(t *testing.T) {
		dir := t.TempDir()
		t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
		a := NewAuthority(dir, func() time.Time { return t0 })
		tok, err := a.Authorize(Hold{AmountMicro: amount, Domain: domain, Agent: agent}, Caps{DailyMicro: 5_000_000})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.MarkSigned(tok); err != nil {
			t.Fatal(err)
		}
		a2 := NewAuthority(dir, func() time.Time { return t0.Add(ReservationTTL + time.Second) })
		// Trigger TTL promote via load (Authorize or Today).
		if _, err := a2.Today(); err != nil {
			t.Fatal(err)
		}
		assertLedgersEqual(t, a2, domain, agent, amount)
	})

	t.Run("CarryMidnight", func(t *testing.T) {
		dir := t.TempDir()
		day1 := time.Date(2026, 10, 1, 23, 59, 0, 0, time.UTC)
		a := NewAuthority(dir, atClock(day1))
		tok, err := a.Authorize(Hold{AmountMicro: amount, Domain: domain, Agent: agent}, Caps{DailyMicro: 5_000_000})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.MarkSigned(tok); err != nil {
			t.Fatal(err)
		}
		day2 := day1.Add(2 * time.Minute)
		a2 := NewAuthority(dir, atClock(day2))
		if _, err := a2.Today(); err != nil {
			t.Fatal(err)
		}
		assertLedgersEqual(t, a2, domain, agent, amount)
	})
}

func TestConcurrentAuthorizeAgentCapNeverExceeds(t *testing.T) {
	a, _ := newTestAuthority(t)
	agentCap := int64(10_000_000) // 10 USDC
	amount := int64(1_000_000)    // 1 USDC
	var mu sync.Mutex
	accepted := 0
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.Authorize(
				Hold{AmountMicro: amount, Domain: "ex.com", Agent: "codex"},
				Caps{DailyMicro: 100_000_000, AgentMicro: agentCap},
			)
			if err == nil {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if accepted != 10 {
		t.Fatalf("want exactly 10 tokens under agent cap, got %d", accepted)
	}
}

func TestAgentCapDoesNotLeakAcrossLabels(t *testing.T) {
	a, _ := newTestAuthority(t)
	tok, err := a.Authorize(
		Hold{AmountMicro: 900_000, Domain: "ex.com", Agent: "codex"},
		Caps{DailyMicro: 1 << 62, AgentMicro: 1_000_000},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Commit(tok); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authorize(
		Hold{AmountMicro: 900_000, Domain: "ex.com", Agent: "codex"},
		Caps{DailyMicro: 1 << 62, AgentMicro: 1_000_000},
	); err != ErrAgentCap {
		t.Fatalf("want ErrAgentCap for same label, got %v", err)
	}
	if _, err := a.Authorize(
		Hold{AmountMicro: 900_000, Domain: "ex.com", Agent: "claude"},
		Caps{DailyMicro: 1 << 62, AgentMicro: 1_000_000},
	); err != nil {
		t.Fatalf("other label must pass: %v", err)
	}
}

func TestUnsignedReleaseFreesAgent(t *testing.T) {
	a, _ := newTestAuthority(t)
	tok, err := a.Authorize(
		Hold{AmountMicro: 900_000, Domain: "ex.com", Agent: "codex"},
		Caps{DailyMicro: 1 << 62, AgentMicro: 1_000_000},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Release(tok); err != nil {
		t.Fatal(err)
	}
	ag, err := a.AgentTotal("codex")
	if err != nil {
		t.Fatal(err)
	}
	if ag != 0 {
		t.Fatalf("unsigned Release must free agent hold, got %d", ag)
	}
	if _, err := a.Authorize(
		Hold{AmountMicro: 900_000, Domain: "ex.com", Agent: "codex"},
		Caps{DailyMicro: 1 << 62, AgentMicro: 1_000_000},
	); err != nil {
		t.Fatalf("after free must authorize again: %v", err)
	}
}

func TestLegacyBudgetFileStartsAgentCounting(t *testing.T) {
	dir := t.TempDir()
	// Pre-55.3 shape: no spent_by_agent_micro.
	raw := `{"day":"2026-10-06","spent_micro":0,"spent_by_domain_micro":{},"reserved":{}}`
	if err := os.WriteFile(filepath.Join(dir, "budget.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	a := NewAuthority(dir, func() time.Time {
		return time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	})
	tok, err := a.Authorize(
		Hold{AmountMicro: 100_000, Domain: "ex.com", Agent: "codex"},
		Caps{DailyMicro: 1 << 62, AgentMicro: 1_000_000},
	)
	if err != nil {
		t.Fatalf("legacy file must load: %v", err)
	}
	if err := a.Commit(tok); err != nil {
		t.Fatal(err)
	}
	st, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	if st.SpentByAgent == nil {
		t.Fatal("SpentByAgent must be initialised for a legacy file")
	}
	if st.SpentByAgent["codex"] != 100_000 {
		t.Fatalf("want agent spent 100000, got %d", st.SpentByAgent["codex"])
	}
}

func TestHandEditedNegativeAgentTotalIsClamped(t *testing.T) {
	dir := t.TempDir()
	a := NewAuthority(dir, nil)
	if _, err := a.Authorize(
		Hold{AmountMicro: 500_000, Domain: "ex.com", Agent: "codex"},
		Caps{DailyMicro: 1 << 62, AgentMicro: 1_000_000},
	); err != nil {
		t.Fatal(err)
	}
	st, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	st.SpentByAgent["codex"] = -999_999
	if err := a.persist(st); err != nil {
		t.Fatal(err)
	}
	b := NewAuthority(dir, nil)
	if _, err := b.Authorize(
		Hold{AmountMicro: 900_000, Domain: "ex.com", Agent: "codex"},
		Caps{DailyMicro: 1 << 62, AgentMicro: 1_000_000},
	); err != ErrAgentCap {
		t.Fatalf("hand-edited negative must not loosen the cap: got %v", err)
	}
}

func TestPromotionSitesMoveAllLedgersSource(t *testing.T) {
	files := []string{
		"authority.go",
		"authority_settle.go",
		"authority_persist.go",
	}
	dir := "."
	if _, err := os.Stat("authority_settle.go"); err != nil {
		dir = "internal/budget"
	}
	var bad []string
	for _, name := range files {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(raw), "\n")
		for i, line := range lines {
			if !strings.Contains(line, "satAddSpent") {
				continue
			}
			// Only promotion into Spent / carry (not domain/agent-only helpers).
			trimmed := strings.TrimSpace(line)
			if !strings.Contains(trimmed, "st.Spent =") && !strings.Contains(trimmed, "carry =") {
				continue
			}
			window := lines[i+1 : min(i+7, len(lines))]
			joined := strings.Join(window, "\n")
			hasDomain := strings.Contains(joined, "SpentByDomain") || strings.Contains(joined, "carryByDomain")
			hasAgent := strings.Contains(joined, "SpentByAgent") || strings.Contains(joined, "carryByAgent")
			if !hasDomain || !hasAgent {
				bad = append(bad, name+":"+strconv.Itoa(i+1))
			}
		}
	}
	if len(bad) > 0 {
		t.Fatalf("promotion sites missing domain+agent ledger in next 6 lines: %v", bad)
	}
}
