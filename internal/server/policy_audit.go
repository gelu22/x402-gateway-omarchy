// Policy/pause audit (011.2, money MUST): cap deltas and kill-switch
// transitions append to audit.log (file sink, durable). Nil-logger safe.
// NEVER: caps/thresholds/paused are ints/bools (no secrets/URLs/payloads
// possible); request bodies are never logged.
package server

import (
	"log/slog"
	"maps"
)

// logPolicyCaps emits one audit line iff the cap or sub-cap changed (micro
// ints / percent; stable shape: both pairs whenever anything changed, so
// equal means unchanged). Returns true when a line was emitted.
func logPolicyCaps(logger *slog.Logger, oldCap, newCap int64, oldSub, newSub int) bool {
	if logger == nil {
		return false
	}
	if oldCap == newCap && oldSub == newSub {
		return false
	}
	logger.Info("policy caps",
		"cap_old_micro", oldCap,
		"cap_new_micro", newCap,
		"subcap_old_percent", oldSub,
		"subcap_new_percent", newSub,
	)
	return true
}

// logPolicyAgentCaps emits "policy agent caps" when the default or map changed.
// Full map is logged only when both sides have ≤16 entries; otherwise only
// counts (avoids huge audit lines).
func logPolicyAgentCaps(logger *slog.Logger, oldDef, newDef int64, oldMap, newMap map[string]int64) bool {
	if logger == nil {
		return false
	}
	if oldDef == newDef && maps.Equal(oldMap, newMap) {
		return false
	}
	attrs := []any{
		"agent_cap_old_micro", oldDef,
		"agent_cap_new_micro", newDef,
		"agent_caps_old_count", len(oldMap),
		"agent_caps_new_count", len(newMap),
	}
	if len(oldMap) <= 16 && len(newMap) <= 16 {
		attrs = append(attrs, "agent_caps_old", oldMap, "agent_caps_new", newMap)
	}
	logger.Info("policy agent caps", attrs...)
	return true
}
