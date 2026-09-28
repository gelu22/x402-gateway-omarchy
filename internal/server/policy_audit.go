// Policy/pause audit (011.2, money MUST): cap deltas and kill-switch
// transitions append to audit.log (file sink, durable). Nil-logger safe.
// NEVER: caps/thresholds/paused are ints/bools (no secrets/URLs/payloads
// possible); request bodies are never logged.
package server

import "log/slog"

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
