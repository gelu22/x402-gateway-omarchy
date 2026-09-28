package cdp

import (
	"sync"
	"time"
)

// Clock skew between this machine and CDP. EIP-3009 authorizations are valid in
// a ±5 minute window (THREAT-MODEL T6), so a machine with a wrong clock fails
// every signature. The `Date` header of a CDP response is a free, per-response
// time reference; /status reports the last fresh observation so the panel can
// warn before payments start failing.
const (
	skewMaxAge    = 5 * time.Minute // older observations say nothing about now
	skewSanityCap = 24 * time.Hour  // an absurd header (proxy, stub) is not a clock
)

type clockSkew struct {
	mu     sync.Mutex
	lastMS int64
	at     time.Time
}

// observe records one server-vs-local comparison. A bogus header is dropped:
// warning about a 48-hour "skew" would be noise, not information.
func (s *clockSkew) observe(serverDate, local time.Time) {
	d := local.Sub(serverDate)
	if d > skewSanityCap || d < -skewSanityCap {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastMS = d.Milliseconds()
	s.at = local
}

// ms returns the last fresh skew (positive = local clock ahead of CDP), or 0
// when there is no observation or the last one is older than maxAge.
func (s *clockSkew) ms(maxAge time.Duration) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.at.IsZero() || time.Since(s.at) > maxAge {
		return 0
	}
	return s.lastMS
}

// ClockSkewMS reports the last fresh local−CDP difference in milliseconds.
// 0 means "unknown", which the panel renders as silence (never a false alarm).
func (c *Client) ClockSkewMS() int64 { return c.skew.ms(skewMaxAge) }
