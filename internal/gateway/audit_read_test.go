package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeAudit(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	p := filepath.Join(dir, "audit.log")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadPaymentHistoryFiltersAndOrders(t *testing.T) {
	dir := t.TempDir()
	p := writeAudit(t, dir,
		`{"time":"2026-01-01T00:00:01Z","msg":"payment audit","amount_micro":1,"domain":"a.example","outcome":"paid","override":false,"agent":""}`,
		`{"time":"2026-01-01T00:00:02Z","msg":"policy caps","daily":1}`,
		`{"time":"2026-01-01T00:00:03Z","msg":"payment audit","amount_micro":2,"domain":"b.example","outcome":"failed:budget_exceeded","override":true,"agent":"opencode"}`,
		`not json`,
		`{"time":"2026-01-01T00:00:04Z","msg":"paused"}`,
		`{"time":"2026-01-01T00:00:05Z","msg":"payment audit","amount_micro":3,"domain":"c.example","outcome":"paid","override":false,"agent":"codex"}`,
	)
	got, trunc, err := ReadPaymentHistory(p, 50, HistoryMaxBytes)
	if err != nil || trunc {
		t.Fatalf("err=%v trunc=%v", err, trunc)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	if got[0].Domain != "c.example" || got[0].Agent != "codex" || got[1].Domain != "b.example" || got[2].AmountMicro != 1 {
		t.Fatalf("order/fields wrong: %+v", got)
	}
}

func TestReadPaymentHistorySkipsBadDomain(t *testing.T) {
	dir := t.TempDir()
	p := writeAudit(t, dir,
		`{"time":"2026-01-01T00:00:01Z","msg":"payment audit","amount_micro":1,"domain":"evil.com/path?x=1","outcome":"paid","override":false,"agent":""}`,
		`{"time":"2026-01-01T00:00:02Z","msg":"payment audit","amount_micro":2,"domain":"ok.example","outcome":"paid","override":false,"agent":""}`,
	)
	got, _, err := ReadPaymentHistory(p, 50, HistoryMaxBytes)
	if err != nil || len(got) != 1 || got[0].Domain != "ok.example" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestReadPaymentHistoryMissingFile(t *testing.T) {
	got, trunc, err := ReadPaymentHistory(filepath.Join(t.TempDir(), "nope.log"), 10, HistoryMaxBytes)
	if err != nil || trunc || len(got) != 0 {
		t.Fatalf("got=%v trunc=%v err=%v", got, trunc, err)
	}
}

func TestReadPaymentHistoryTruncatesLargeFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "audit.log")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	line := `{"time":"2026-01-01T00:00:00Z","msg":"noise","x":"` + strings.Repeat("a", 200) + `"}` + "\n"
	for i := 0; i < 20_000; i++ {
		if _, err := f.WriteString(line); err != nil {
			t.Fatal(err)
		}
	}
	pay := `{"time":"2026-01-01T12:00:00Z","msg":"payment audit","amount_micro":9,"domain":"tail.example","outcome":"paid","override":false,"agent":""}` + "\n"
	if _, err := f.WriteString(pay); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	got, trunc, err := ReadPaymentHistory(p, 50, HistoryMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !trunc {
		t.Fatal("want truncated=true")
	}
	if len(got) != 1 || got[0].Domain != "tail.example" {
		t.Fatalf("got=%+v", got)
	}
}

func FuzzParseAuditLine(f *testing.F) {
	f.Add(`{"time":"2026-01-01T00:00:00Z","msg":"payment audit","amount_micro":1,"domain":"a.b","outcome":"paid","override":false,"agent":"x"}`)
	f.Add(`{}`)
	f.Add(`not json`)
	f.Fuzz(func(t *testing.T, line string) {
		_, _ = parsePaymentAuditLine(line)
	})
}
