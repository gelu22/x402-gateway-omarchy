// Command gateway --status: runStatus + usd.
// Helper functions (fetchStatus, tailFile, etc.) are in status_helpers.go.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gateway/internal/config"
	"gateway/internal/policy"
	"gateway/internal/spend"
)

// runStatus prints the snapshot to stdout and returns the process exit code.
// Errors go to stderr so the snapshot stays pipeable.
func runStatus(cfg *config.Config) int {
	var b bytes.Buffer
	fail := func(format string, args ...any) int {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
		return 1
	}

	fmt.Fprintf(&b, "gateway %s\n", cfg.Version)

	st, err := fetchStatus(cfg.SocketPath)
	if err != nil {
		return fail("daemon: unreachable via %s (%v)", cfg.SocketPath, err)
	}
	state := strField(st, "state")
	if state == "" {
		state = "unknown"
	}
	fmt.Fprintf(&b, "daemon: %s\n", state)

	pol, err := policy.Load(cfg.StateDir)
	if err != nil {
		return fail("policy %s/policy.json unreadable: %v", cfg.StateDir, err)
	}
	if err := checkSpendValid(cfg.StateDir); err != nil {
		return fail("%v", err)
	}
	tr := spend.NewTracker(cfg.StateDir)
	spent, err := tr.Today()
	if err != nil {
		return fail("spend %s/spend.json unreadable: %v", cfg.StateDir, err)
	}
	fmt.Fprintf(&b, "budget: $%s / $%s spent\n", usd(spent), usd(pol.DailyCapMicro))

	auditPath := filepath.Join(cfg.StateDir, "audit.log")
	lines, err := tailFile(auditPath, auditTailLines)
	if err != nil {
		fmt.Fprintf(&b, "audit: no entries (%s missing)\n", auditPath)
	} else if len(lines) == 0 {
		fmt.Fprintf(&b, "audit: no entries yet\n")
	} else {
		fmt.Fprintf(&b, "audit: last %d line(s):\n", len(lines))
		for _, ln := range lines {
			fmt.Fprintf(&b, "  %s\n", ln)
		}
	}

	home, _ := os.LookupEnv("HOME")
	network, remembered, err := readPluginConfig(home)
	if err != nil {
		return fail("%v", err)
	}
	fmt.Fprintf(&b, "config: network=%s remembered=%d\n", network, remembered)

	fmt.Print(b.String())
	return 0
}
