package gateway

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
)

// SsrfGuard implements dial-time SSRF protection by checking resolved IPs
// against a deny-list and then connecting ONLY to a checked IP (pin).
// Pinning closes the DNS-rebinding window between check and connect within one
// attempt: no second lookup ever happens after validation. It does NOT protect
// against a malicious but public IP — that was never checkable by address.
type SsrfGuard struct {
	allowPrivate atomic.Bool
	// lookupIPAddr resolves host; nil = system resolver. Overridden in tests
	// to simulate rebinding (different IPs across lookups).
	lookupIPAddr func(ctx context.Context, host string) ([]net.IPAddr, error)
}

// checkIP validates that the given IP is not in a private/reserved range.
// Returns an error if the IP is blocked by SSRF policy.
// Unmap first: LookupIPAddr often returns 16-byte IPv4-mapped addresses
// (::ffff:x.x.x.x). Is4() is false for those, so the CGNAT gate would miss
// 100.64.0.0/10 without Unmap (marketplace #10084 / T2).
func checkIP(ip netip.Addr) error {
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return fmt.Errorf("SSRF: loopback address %s", ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("SSRF: private address %s", ip)
	}
	if ip.IsLinkLocalUnicast() {
		return fmt.Errorf("SSRF: link-local address %s", ip)
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("SSRF: unspecified address %s", ip)
	}
	// Block CGNAT 100.64.0.0/10 (RFC 6598) - used by Tailscale etc.
	// Public services are not reachable via CGNAT.
	if ip.Is4() {
		if ip.As4()[0] == 100 && ip.As4()[1] >= 64 && ip.As4()[1] <= 127 {
			return fmt.Errorf("SSRF: CGNAT address %s", ip)
		}
	}
	// Block multicast
	if ip.IsMulticast() {
		return fmt.Errorf("SSRF: multicast address %s", ip)
	}
	return nil
}

// DialContext wraps the given dialer with SSRF protection at connect time.
// The returned dialer checks the resolved IP against checkIP before connecting.
func DialContext(g *SsrfGuard, baseDialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	if baseDialer == nil {
		baseDialer = &net.Dialer{}
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		// AllowPrivate bypasses SSRF checks (dev/test only)
		if g.allowPrivate.Load() {
			return baseDialer.DialContext(ctx, network, address)
		}

		// Use dialWithCheck which resolves once, checks IPs, then dials the pin.
		return dialWithCheck(ctx, baseDialer.DialContext, network, address, g)
	}
}

// dialWithCheck resolves once, validates ALL resolved IPs, then dials the
// first checked one (pin). No second lookup happens after validation, so a
// hostile resolver cannot swap the address between check and connect. TLS SNI
// and the HTTP Host header keep coming from the original hostname: this layer
// only picks the TCP endpoint, the TLS/HTTP layers above still see the URL
// host (Go http.Transport derives ServerName from the request, not the dialed
// address).
func dialWithCheck(ctx context.Context, dial func(ctx context.Context, network, addr string) (net.Conn, error), network, address string, g *SsrfGuard) (net.Conn, error) {
	if g == nil || dial == nil {
		return nil, fmt.Errorf("SSRF: nil guard or dialer")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("SSRF: invalid address %q: %w", address, err)
	}

	// Parse port
	_, err = strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("SSRF: invalid port %q: %w", port, err)
	}

	// Reject IPv6 zones explicitly: they do not survive AddrFromSlice, so a
	// zoned host could otherwise dial a different address than the checked one.
	if strings.Contains(host, "%") {
		return nil, fmt.Errorf("SSRF: zoned address %q not allowed", host)
	}

	// Single lookup for this attempt.
	lookup := g.lookupIPAddr
	if lookup == nil {
		var resolver net.Resolver
		lookup = resolver.LookupIPAddr
	}
	addrs, err := lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("SSRF: DNS resolution failed for %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("SSRF: no addresses for %s", host)
	}

	// Check each resolved IP (zones rejected too: String() would drop them,
	// so a zoned result must never reach the dial string).
	for _, a := range addrs {
		if a.Zone != "" {
			return nil, fmt.Errorf("SSRF: zoned address %q not allowed", a.String())
		}
		ip, ok := netip.AddrFromSlice(a.IP)
		if !ok {
			return nil, fmt.Errorf("SSRF: invalid IP %s", a.IP)
		}
		if err := checkIP(ip); err != nil {
			return nil, err
		}
	}

	// Pin: dial the first checked IP. No fallback to unvalidated addresses;
	// a dial error surfaces like any other dial failure.
	pinned := net.JoinHostPort(addrs[0].IP.String(), port)
	return dial(ctx, network, pinned)
}

// SetAllowPrivate enables/disables the AllowPrivate bypass (for dev/test).
func (g *SsrfGuard) SetAllowPrivate(allow bool) {
	g.allowPrivate.Store(allow)
}

// validateTarget blocks non-http(s) and local/private targets (SSRF guard).
func validateTarget(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("local host %q not allowed", host)
	}
	for _, p := range []string{"127.", "10.", "192.168.", "169.254.", "0."} {
		if strings.HasPrefix(host, p) {
			return fmt.Errorf("private address %q not allowed", host)
		}
	}
	if strings.HasPrefix(host, "172.") {
		if second, ok := strings.CutPrefix(host, "172."); ok {
			var o int
			if _, err := fmt.Sscanf(second, "%d", &o); err == nil && o >= 16 && o <= 31 {
				return fmt.Errorf("private address %q not allowed", host)
			}
		}
	}
	if strings.Contains(host, ":") && (host == "::1" || strings.HasPrefix(host, "fe80:") || strings.HasPrefix(host, "fc") || strings.HasPrefix(host, "fd")) {
		return fmt.Errorf("local IPv6 %q not allowed", host)
	}
	return nil
}
