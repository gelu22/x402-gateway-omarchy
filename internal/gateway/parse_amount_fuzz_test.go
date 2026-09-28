package gateway

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// canonicalAmount is exactly what strconv.ParseInt(s, 10, 64) accepts:
// optional sign + ASCII digits. Anything else must be a hard error —
// Sscanf %d silently truncated "1e9"->1 and "0x1000"->0 (016.6a).
var canonicalAmount = regexp.MustCompile(`^[+-]?[0-9]+$`)

func FuzzParseAmountMicro(f *testing.F) {
	// Valid seeds
	f.Add("1000000")
	f.Add("0")
	f.Add("250000")
	f.Add("2000")
	f.Add("1000000000000")

	// Invalid seeds
	f.Add("")
	f.Add("-1")
	f.Add("-1000")
	f.Add("1.5")
	f.Add("1e6")
	f.Add("1e9")
	f.Add("0x1000")
	f.Add("abc")
	f.Add("1,000,000")
	f.Add("1_000_000")
	f.Add("  1000000  ")
	f.Add("1000000\n")
	f.Add("1000000\r")
	f.Add("1000000\t")
	// int64 edges + explicit plus (016.4 money-math hardening)
	f.Add("9223372036854775807")
	f.Add("9223372036854775808")
	f.Add("+5")

	f.Fuzz(func(t *testing.T, input string) {
		v, err := parseAmountMicro(input)
		trimmed := strings.TrimSpace(input)
		if err != nil {
			// Errors allowed only for non-canonical input or int64 overflow.
			if canonicalAmount.MatchString(trimmed) {
				if _, oerr := strconv.ParseInt(trimmed, 10, 64); oerr == nil {
					t.Fatalf("parseAmountMicro(%q) errored on canonical in-range input: %v", input, err)
				}
			}
			return
		}
		// Success pins no-silent-truncation: canonical decimal in, same value out.
		if !canonicalAmount.MatchString(trimmed) {
			t.Fatalf("parseAmountMicro(%q) = %d: accepted non-canonical input (truncation risk)", input, v)
		}
		if want, _ := strconv.ParseInt(trimmed, 10, 64); v != want {
			t.Fatalf("parseAmountMicro(%q) = %d, want %d", input, v, want)
		}
	})
}
