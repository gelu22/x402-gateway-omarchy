package gateway

import (
	"log/slog"
	"sort"

	"gateway/internal/policy"
)

const maxStatusAgents = 32

// agentSpendRows builds /status.agents from the day's ledger ∪ policy map
// (top 32 by spent). Nil Budget → nil (omitempty). Ledger read errors → warn + nil.
func (g *Gateway) agentSpendRows(pol *policy.Policy) []AgentSpend {
	if g.Budget == nil {
		return nil
	}
	totals, err := g.Budget.AgentTotals()
	if err != nil {
		if g.Logger != nil {
			g.Logger.Warn("status agents ledger", slog.Any("err", err))
		}
		return nil
	}
	labels := make(map[string]struct{})
	for label, spent := range totals {
		if label == "" && spent == 0 {
			continue // empty bucket only when it has spend (or policy map, which cannot key "")
		}
		labels[label] = struct{}{}
	}
	if pol != nil {
		for label := range pol.AgentCapsMicro {
			labels[label] = struct{}{}
		}
	}
	rows := make([]AgentSpend, 0, len(labels))
	for label := range labels {
		capMicro := int64(0)
		if pol != nil {
			capMicro = pol.AgentCapMicro(label)
		}
		rows = append(rows, AgentSpend{
			Label:           label,
			SpentTodayMicro: totals[label],
			CapMicro:        capMicro,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].SpentTodayMicro != rows[j].SpentTodayMicro {
			return rows[i].SpentTodayMicro > rows[j].SpentTodayMicro
		}
		return rows[i].Label < rows[j].Label
	})
	if len(rows) > maxStatusAgents {
		rows = rows[:maxStatusAgents]
	}
	if len(rows) == 0 {
		return nil
	}
	return rows
}
