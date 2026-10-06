// audit_read.go — read-only payment history from audit.log (T8: domain only).
package gateway

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"gateway/internal/agentlabel"
)

// HistoryMaxBytes caps how much of audit.log we read from the end.
const HistoryMaxBytes = 256 << 10

// AuditEntry is one validated payment-audit row for GET /history.
type AuditEntry struct {
	Time        time.Time `json:"time"`
	AmountMicro int64     `json:"amount_micro"`
	Domain      string    `json:"domain"`
	Outcome     string    `json:"outcome"`
	Override    bool      `json:"override"`
	Agent       string    `json:"agent"`
}

var (
	domainRE  = regexp.MustCompile(`^[a-z0-9.-]{1,253}$`)
	outcomeRE = regexp.MustCompile(`^(paid|failed:[a-z_]+)$`)
)

// ReadPaymentHistory returns the newest payment-audit entries (newest first).
// truncated is true if the file was larger than maxBytes or more than limit matched.
func ReadPaymentHistory(path string, limit int, maxBytes int64) ([]AuditEntry, bool, error) {
	if limit < 1 {
		limit = 50
	}
	if maxBytes < 1 {
		maxBytes = HistoryMaxBytes
	}
	st, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	size := st.Size()
	f, err := os.Open(path) // #nosec G304 — path is daemon state dir, not user taint
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()

	var start int64
	truncatedFile := false
	if size > maxBytes {
		start = size - maxBytes
		truncatedFile = true
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, false, err
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	var lines []string
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first && start > 0 {
			first = false
			continue // drop partial first line after mid-file seek
		}
		first = false
		lines = append(lines, line)
	}
	if err := sc.Err(); err != nil {
		// Too-long line: skip remainder; still return what we have.
		if !errors.Is(err, bufio.ErrTooLong) {
			return nil, false, err
		}
	}

	var out []AuditEntry
	for i := len(lines) - 1; i >= 0; i-- {
		e, ok := parsePaymentAuditLine(lines[i])
		if !ok {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			// More payment lines may exist earlier in the window or file.
			more := i > 0 || truncatedFile
			return out, more || truncatedFile, nil
		}
	}
	return out, truncatedFile, nil
}

func parsePaymentAuditLine(line string) (AuditEntry, bool) {
	var raw struct {
		Time        string `json:"time"`
		Msg         string `json:"msg"`
		AmountMicro *int64 `json:"amount_micro"`
		Domain      string `json:"domain"`
		Outcome     string `json:"outcome"`
		Override    bool   `json:"override"`
		Agent       string `json:"agent"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return AuditEntry{}, false
	}
	if raw.Msg != "payment audit" || raw.AmountMicro == nil || *raw.AmountMicro < 0 {
		return AuditEntry{}, false
	}
	dom := strings.ToLower(raw.Domain)
	if dom != "" && !domainRE.MatchString(dom) {
		return AuditEntry{}, false
	}
	if raw.Agent != "" && !agentlabel.Valid(raw.Agent) {
		return AuditEntry{}, false
	}
	if !outcomeRE.MatchString(raw.Outcome) {
		return AuditEntry{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, raw.Time)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, raw.Time)
		if err != nil {
			return AuditEntry{}, false
		}
	}
	return AuditEntry{
		Time:        ts,
		AmountMicro: *raw.AmountMicro,
		Domain:      dom,
		Outcome:     raw.Outcome,
		Override:    raw.Override,
		Agent:       raw.Agent,
	}, true
}
