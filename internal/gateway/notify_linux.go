//go:build linux

package gateway

import (
	"os"
	"os/exec"
)

// notifyDisabledEnv silences desktop notifications. Test and dev runs that
// deliberately provoke a budget exhaustion (scripts/e2e-test.sh --override)
// would otherwise pop user-facing alerts with test numbers; the daemon has to
// be started with GATEWAY_NOTIFY=0 for that, since the env belongs to it.
const notifyDisabledEnv = "GATEWAY_NOTIFY"

// notifyUrgency is "normal", not a persistent urgency: Omarchy persists and
// replays such a toast after a shell restart, so a stale budget alert would pop
// up again on every restart. "normal" expires on its own and moves to history.
const notifyUrgency = "normal"

// notifyTTLms is an explicit TTL, so expiry does not depend on the desktop
// environment's default per-urgency duration. 10 s is enough to notice the
// toast and guarantees it vanishes instead of lingering as a live popup.
const notifyTTLms = "10000"

// notifyArgs returns the full notify-send argv. Pure, so the "transient
// contract" (normal + TTL) is pinned by a unit test without spawning a process.
func notifyArgs(title, body string) []string {
	return []string{"notify-send", "-a", "x402 Gateway", "-u", notifyUrgency, "-t", notifyTTLms, title, body}
}

// desktopNotify posts a system notification via notify-send (Omarchy ships
// a notification daemon). Missing binary → silently skipped.
func desktopNotify(title, body string) {
	if os.Getenv(notifyDisabledEnv) == "0" {
		return
	}
	args := notifyArgs(title, body)
	_ = exec.Command(args[0], args[1:]...).Run() // #nosec G204 -- argv is fixed strings + formatted floats; no seller/user-controlled text
}
