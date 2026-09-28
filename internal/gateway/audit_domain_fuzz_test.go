package gateway

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// noURLStructure rejects any input that contains URL markers except for IPv6
// colons (net/url.Hostname returns bare IPv6 which naturally contains ":").
// A hostname with a port would have a trailing ":" or a ":" after the last
// IPv6 segment — Hostname strips port so that never reaches us.
var noURLStructure = regexp.MustCompile(`[/?@]`)

func FuzzAuditDomain(f *testing.F) {
	// Normal URLs
	f.Add("https://api.example.com/v1/x402-test")
	f.Add("https://node4all.com:443/path?query=value")
	f.Add("https://user:pass@host.com/a")
	// Edge hosts
	f.Add("localhost")
	f.Add("127.0.0.1")
	f.Add("10.0.0.1")
	f.Add("172.16.0.1")
	f.Add("::1")
	f.Add("fe80::1")
	// Edge inputs
	f.Add("")
	f.Add("not-a-url")
	f.Add("://malformed")
	f.Add("ftp://example.com")
	f.Add("https://example.com/path with spaces")
	f.Add("https://EXAMPLE.COM/UPPER")
	f.Add("https://a.b.c.d.e.f.g.com/")
	f.Add("https://[::1]/v1")
	f.Add("https://[2001:db8::1]:443/")
	f.Add("///")
	f.Add("http://[::ffff:127.0.0.1]/")
	// MaxInt64-like garbage
	f.Add("https://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.com/")
	f.Add("\x00\x01\x02\x7f\x80\xff")

	f.Fuzz(func(t *testing.T, input string) {
		got := auditDomain(input)

		// Invariant 1: output never contains URL-structure markers.
		// A hostname alone has no ://, /, ?, @, or : (port stripped).
		if noURLStructure.MatchString(got) {
			t.Fatalf("auditDomain(%q) = %q: contains URL-structure marker (T2: secret/URL leak)", input, got)
		}

		// Invariant 2: idempotent — feeding the hostname back yields itself
		// (only for plain hostnames; IPv6 addresses contain ":" which breaks
		// the re-wrap "https://" + hostname + "/" idempotency test).
		if got != "" && !strings.Contains(got, ":") && !strings.Contains(got, "[") && !strings.Contains(got, "]") {
			u, err := url.Parse("https://" + got + "/")
			if err != nil {
				t.Fatalf("auditDomain(%q) = %q: result is not a valid hostname (err=%v)", input, got, err)
			}
			if u.Hostname() != got {
				t.Fatalf("auditDomain(%q) = %q: not idempotent (u.Hostname()=%q)", input, got, u.Hostname())
			}
		}
	})
}
