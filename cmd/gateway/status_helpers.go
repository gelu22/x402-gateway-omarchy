// Status helpers: fetchStatus, tailFile, stripLineComments, readPluginConfig, strField, checkSpendValid.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// auditTailLines caps the audit dump: a glance, not a full file dump.
const auditTailLines = 10

// usd formats micro-USDC exactly (no float): 1234567 -> "1.23".
func usd(micro int64) string {
	sign := ""
	if micro < 0 {
		sign, micro = "-", -micro
	}
	return fmt.Sprintf("%s%d.%02d", sign, micro/1_000_000, (micro%1_000_000)/10_000)
}

// fetchStatus GETs /status over the unix socket into a generic map so an
// older daemon missing new fields degrades instead of crashing.
func fetchStatus(socketPath string) (map[string]any, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}
	resp, err := client.Get("http://localhost/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d from daemon", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// tailFile returns the last n non-empty lines of a file, reading at most the
// trailing 64 KB (a huge audit.log must not OOM a diagnostic) and truncating
// each line to 512 chars for terminal sanity.
func tailFile(path string, n int) ([]string, error) {
	const maxTailBytes = 64 * 1024
	const maxLineChars = 512
	f, err := os.Open(path) // #nosec G304 -- path joins user-owned stateDir (same-user trust); no remote input
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	off := st.Size() - maxTailBytes
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, 0); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	// A mid-line seek start yields a partial first line — drop it when we
	// skipped bytes, so output never shows a truncated record.
	lines := strings.Split(string(raw), "\n")
	if off > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	var out []string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if len(ln) > maxLineChars {
			ln = ln[:maxLineChars] + "…"
		}
		out = append(out, ln)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}

// stripLineComments drops whole-line // comments (plugin config is JSONC,
// like Model.js parseJsonc) so encoding/json can parse the rest.
func stripLineComments(raw string) string {
	var out []string
	for _, ln := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "//") {
			continue
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}

// readPluginConfig returns the payment network + remembered-URL count.
// Corrupt JSON is an explicit error naming the file (never silent, never panic).
// Empty HOME is an explicit error too: without it the path would go relative
// and could accidentally read a repo-local file.
func readPluginConfig(home string) (network string, remembered int, err error) {
	if home == "" {
		return "", 0, fmt.Errorf("HOME unset: cannot locate plugin config")
	}
	path := filepath.Join(home, ".config", "omarchy", "x402-gateway", "config.json")
	raw, err := os.ReadFile(path) // #nosec G304 -- path is stateDir or $HOME config (same-user trust); no remote input
	if err != nil {
		return "", 0, fmt.Errorf("plugin config %s: %w", path, err)
	}
	var cfg struct {
		PaymentNetwork string `json:"paymentNetwork"`
		RememberedURLs []struct {
			URL string `json:"url"`
		} `json:"rememberedUrls"`
	}
	if err := json.Unmarshal([]byte(stripLineComments(string(raw))), &cfg); err != nil {
		return "", 0, fmt.Errorf("plugin config %s: %w", path, err)
	}
	return cfg.PaymentNetwork, len(cfg.RememberedURLs), nil
}

// strField reads an optional string field (missing/wrong type -> "").
func strField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// checkSpendValid fails on corrupt spend.json instead of silently reporting
// $0.00: spend.load() intentionally returns fresh zeros (daemon fail-safe),
// but a diagnostic must never present a broken ledger as empty.
func checkSpendValid(stateDir string) error {
	raw, err := os.ReadFile(filepath.Join(stateDir, "spend.json")) // #nosec G304 -- path joins user-owned stateDir (same-user trust); no remote input
	if os.IsNotExist(err) {
		return nil // fresh install: zeros are honest
	}
	if err != nil {
		return err
	}
	var probe struct {
		Day string `json:"day"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("spend.json corrupt: %w", err)
	}
	return nil
}
