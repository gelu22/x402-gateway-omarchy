package gateway

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestCheckIP_Loopback(t *testing.T) {
	ip := netip.MustParseAddr("127.0.0.1")
	if err := checkIP(ip); err == nil {
		t.Errorf("expected error for loopback, got nil")
	}
}

func TestCheckIP_PrivateRanges(t *testing.T) {
	tests := []struct {
		name string
		ip   string
	}{
		{"10.0.0.1", "10.0.0.1"},
		{"192.168.1.1", "192.168.1.1"},
		{"172.16.0.1", "172.16.0.1"},
		{"172.31.255.255", "172.31.255.255"},
		{"169.254.0.1", "169.254.0.1"},
		{"0.0.0.0", "0.0.0.0"},
		{"::1", "::1"},
		{"fe80::1", "fe80::1"},
		{"fc00::1", "fc00::1"},
		{"fd00::1", "fd00::1"},
		{"100.64.0.1", "100.64.0.1"},
		{"100.127.255.255", "100.127.255.255"},
		{"224.0.0.1", "224.0.0.1"},
		{"ff02::1", "ff02::1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := netip.MustParseAddr(tc.ip)
			if err := checkIP(ip); err == nil {
				t.Errorf("checkIP(%s) expected error, got nil", tc.ip)
			}
		})
	}
}

func TestCheckIP_PublicAllowed(t *testing.T) {
	tests := []struct {
		name string
		ip   string
	}{
		{"google-dns", "8.8.8.8"},
		{"cloudflare-dns", "1.1.1.1"},
		{"example", "93.184.216.34"},
		{"ipv6-google", "2001:4860:4860::8888"},
		{"ipv6-cloudflare", "2606:4700:4700::1111"},
		{"100.128.0.1", "100.128.0.1"}, // Just outside CGNAT
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := netip.MustParseAddr(tc.ip)
			if err := checkIP(ip); err != nil {
				t.Errorf("checkIP(%s) unexpected error: %v", tc.ip, err)
			}
		})
	}
}

func TestDialContext_AllowPrivate(t *testing.T) {
	g := &SsrfGuard{}
	g.SetAllowPrivate(true)

	dialer := DialContext(g, &net.Dialer{Timeout: 5 * time.Second})

	// This should not call the resolver since AllowPrivate is true
	// We can't easily test the full dial without a real server,
	// but we can verify the dialer function is returned
	if dialer == nil {
		t.Fatal("DialContext returned nil")
	}
}

func TestDialContext_BlocksLocalhost(t *testing.T) {
	g := &SsrfGuard{}
	g.SetAllowPrivate(false)

	dialer := DialContext(g, &net.Dialer{Timeout: 5 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Try to dial localhost - should be blocked
	_, err := dialer(ctx, "tcp", "localhost:80")
	if err == nil {
		t.Fatal("expected error for localhost dial, got nil")
	}
	if err.Error() != "SSRF: loopback address 127.0.0.1" && err.Error() != "SSRF: loopback address ::1" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckIP_IPv6(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"loopback", "::1", true},
		{"link-local", "fe80::1", true},
		{"ULA-fc", "fc00::1", true},
		{"ULA-fd", "fd00::1", true},
		{"multicast", "ff02::1", true},
		{"global-unicast", "2001:db8::1", false},
		{"google-dns", "2001:4860:4860::8888", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := netip.MustParseAddr(tc.ip)
			err := checkIP(ip)
			if tc.blocked && err == nil {
				t.Errorf("checkIP(%s) expected error, got nil", tc.ip)
			}
			if !tc.blocked && err != nil {
				t.Errorf("checkIP(%s) unexpected error: %v", tc.ip, err)
			}
		})
	}
}

// fakeLookup returns scripted results per call, simulating a hostile resolver
// that swaps addresses between lookups (DNS rebinding).
type fakeLookup struct {
	calls int
	ips   [][]net.IPAddr
}

func (f *fakeLookup) lookup(_ context.Context, _ string) ([]net.IPAddr, error) {
	defer func() { f.calls++ }()
	if f.calls < len(f.ips) {
		return f.ips[f.calls], nil
	}
	return f.ips[len(f.ips)-1], nil
}

// recordDialer captures the addr it was asked to dial and returns a Pipe
// conn: no network, fully deterministic.
type recordDialer struct {
	addrs []string
}

func (d *recordDialer) dial(_ context.Context, _ string, addr string) (net.Conn, error) {
	d.addrs = append(d.addrs, addr)
	c1, c2 := net.Pipe()
	_ = c2.Close()
	return c1, nil
}

func pinGuard(ips ...string) (*SsrfGuard, *fakeLookup) {
	list := make([]net.IPAddr, len(ips))
	for i, s := range ips {
		list[i] = net.IPAddr{IP: net.ParseIP(s)}
	}
	lookup := &fakeLookup{ips: [][]net.IPAddr{list}}
	return &SsrfGuard{lookupIPAddr: lookup.lookup}, lookup
}

func TestDialPin_RebindingProof(t *testing.T) {
	// First lookup (check phase) sees a clean IP; any later lookup would see
	// a private one. Pinning must use exactly one lookup and dial the checked IP.
	lookup := &fakeLookup{ips: [][]net.IPAddr{
		{{IP: net.ParseIP("192.0.2.1")}},
		{{IP: net.ParseIP("10.9.9.9")}},
	}}
	rd := &recordDialer{}
	g := &SsrfGuard{lookupIPAddr: lookup.lookup}

	conn, err := dialWithCheck(context.Background(), rd.dial, "tcp", "seller.example:443", g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if conn == nil {
		t.Fatal("want conn, got nil")
	}
	defer conn.Close()
	if lookup.calls != 1 {
		t.Fatalf("lookups = %d, want exactly 1 (no re-resolve after check)", lookup.calls)
	}
	if len(rd.addrs) != 1 || rd.addrs[0] != "192.0.2.1:443" {
		t.Fatalf("dialed %v, want [192.0.2.1:443] (checked IP, never the swapped one)", rd.addrs)
	}
}

func TestDialPin_MixedListRejected(t *testing.T) {
	// One private IP in the list rejects the whole attempt — no dial at all,
	// not even to the clean address.
	g, _ := pinGuard("93.184.216.34", "10.0.0.1")
	rd := &recordDialer{}
	_, err := dialWithCheck(context.Background(), rd.dial, "tcp", "x.example:443", g)
	if err == nil || !strings.Contains(err.Error(), "private") {
		t.Fatalf("want private-address error, got %v", err)
	}
	if len(rd.addrs) != 0 {
		t.Fatalf("dialer called %v, want no dial on mixed list", rd.addrs)
	}
}

func TestDialPin_LiteralIP(t *testing.T) {
	g, lookup := pinGuard("93.184.216.34")
	rd := &recordDialer{}
	_, err := dialWithCheck(context.Background(), rd.dial, "tcp", "93.184.216.34:80", g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lookup.calls != 1 || len(rd.addrs) != 1 || rd.addrs[0] != "93.184.216.34:80" {
		t.Fatalf("lookups=%d dialed=%v, want 1 lookup and [93.184.216.34:80]", lookup.calls, rd.addrs)
	}
}

func TestDialPin_LiteralPrivate(t *testing.T) {
	// Literal private IPs (plain and 4-in-6 mapped) must be blocked with zero dials.
	for _, target := range []string{"10.0.0.1:80", "[::ffff:10.0.0.1]:80"} {
		host, _, _ := net.SplitHostPort(target)
		g, _ := pinGuard(host)
		rd := &recordDialer{}
		_, err := dialWithCheck(context.Background(), rd.dial, "tcp", target, g)
		if err == nil {
			t.Fatalf("want block for %s, got nil", target)
		}
		if len(rd.addrs) != 0 {
			t.Fatalf("dialer called %v for %s, want no dial", rd.addrs, target)
		}
	}
}

func TestDialPin_IPv6Brackets(t *testing.T) {
	g, _ := pinGuard("2606:4700:4700::1111")
	rd := &recordDialer{}
	_, err := dialWithCheck(context.Background(), rd.dial, "tcp", "[2606:4700:4700::1111]:443", g)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rd.addrs) != 1 || rd.addrs[0] != "[2606:4700:4700::1111]:443" {
		t.Fatalf("dialed %v, want bracketed IPv6 pin", rd.addrs)
	}
}

func TestDialPin_ZoneRejected(t *testing.T) {
	g, lookup := pinGuard("fe80::1")
	rd := &recordDialer{}
	_, err := dialWithCheck(context.Background(), rd.dial, "tcp", "[fe80::1%eth0]:80", g)
	if err == nil || !strings.Contains(err.Error(), "zoned") {
		t.Fatalf("want zoned-address error, got %v", err)
	}
	if lookup.calls != 0 || len(rd.addrs) != 0 {
		t.Fatal("want no lookup and no dial for zoned address")
	}
}

func TestDialPin_EmptyList(t *testing.T) {
	lookup := &fakeLookup{ips: [][]net.IPAddr{{}}}
	rd := &recordDialer{}
	g := &SsrfGuard{lookupIPAddr: lookup.lookup}
	_, err := dialWithCheck(context.Background(), rd.dial, "tcp", "x.example:443", g)
	if err == nil || !strings.Contains(err.Error(), "no addresses") {
		t.Fatalf("want no-addresses error, got %v", err)
	}
	if len(rd.addrs) != 0 {
		t.Fatal("want no dial on empty list")
	}
}

// 34.4: validateTarget is the PRE-FLIGHT filter (scheme/format/private literals)
// and runs only in production (AllowPrivate=false, fetch.go:31-35); the
// authoritative guard is the dial-time checkIP. It had 0% coverage, so a
// regression would only be caught in production.

func TestValidateTarget(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"https public", "https://api.example.com/x", false},
		{"http public", "http://example.com", false},
		{"uppercase host and port", "HTTP://EXAMPLE.COM:8080/", false},
		{"172.32 is outside the private block", "http://172.32.0.1/", false},
		{"public IPv6", "http://[2606:4700::1111]/", false},
		{"file scheme", "file:///etc/passwd", true},
		{"ftp scheme", "ftp://example.com", true},
		{"no scheme", "example.com", true},
		{"empty host", "http:///x", true},
		{"localhost", "http://localhost/", true},
		{"subdomain of localhost", "http://foo.localhost/", true},
		{"loopback v4", "http://127.0.0.1/", true},
		{"private 10/8", "http://10.0.0.1/", true},
		{"private 172.16", "http://172.16.0.1/", true},
		{"private 172.31", "http://172.31.255.1/", true},
		{"private 192.168", "http://192.168.1.1/", true},
		{"cloud metadata 169.254", "http://169.254.169.254/", true},
		{"unspecified v4", "http://0.0.0.0/", true},
		{"loopback v6", "http://[::1]/", true},
		{"link-local v6", "http://[fe80::1]/", true},
		{"unique-local fc", "http://[fc00::1]/", true},
		{"unique-local fd", "http://[fd00::1]/", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTarget(tc.raw)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateTarget(%q) error = %v, wantErr %v", tc.raw, err, tc.wantErr)
			}
		})
	}
}

// The boundary between the two layers: an IPv4-mapped loopback passes the
// pre-flight filter (no prefix matches) but is blocked at dial time by checkIP.
// Pinning it prevents "fixing" validateTarget by weakening checkIP instead.
func TestMappedIPv4LoopbackBlockedAtDialGuard(t *testing.T) {
	if err := validateTarget("http://[::ffff:127.0.0.1]/"); err != nil {
		t.Fatalf("pre-flight filter is not the authoritative guard, got %v", err)
	}
	if err := checkIP(netip.MustParseAddr("::ffff:127.0.0.1")); err == nil {
		t.Fatal("dial-time guard must reject IPv4-mapped loopback")
	}
}

// 34.4: with AllowPrivate=false (production) a private target is refused before
// any signing: no WalletSecret call, nothing settled.
func TestFetchRefusesPrivateTargetBeforeSigning(t *testing.T) {
	gw, signer := newGateway(t, 5_000_000, 0)
	gw.AllowPrivate = false

	_, err := gw.Fetch(context.Background(), http.MethodGet, "http://127.0.0.1:1/x", nil, nil)
	if !errors.Is(err, ErrBadTarget) {
		t.Fatalf("err = %v, want ErrBadTarget", err)
	}
	if got := signer.signCalls.Load(); got != 0 {
		t.Fatalf("signCalls = %d, want 0 (must refuse before signing)", got)
	}
	if spent, _ := gw.Budget.Today(); spent != 0 {
		t.Fatalf("spend = %d, want 0 (nothing settled)", spent)
	}
}
