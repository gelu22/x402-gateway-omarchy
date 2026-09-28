package cdp

import (
	"testing"
	"time"
)

func TestClockSkewObserveAndRead(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		serverDate time.Time
		local      time.Time
		want       int64
	}{
		{"in sync", now, now, 0},
		{"local ahead by 90s", now.Add(-90 * time.Second), now, 90000},
		{"local behind by 90s", now.Add(90 * time.Second), now, -90000},
		{"bogus header 48h ahead", now.Add(48 * time.Hour), now, 0},
		{"bogus header 48h behind", now.Add(-48 * time.Hour), now, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var s clockSkew
			s.observe(tc.serverDate, tc.local)
			if got := s.ms(skewMaxAge); got != tc.want {
				t.Errorf("ms() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestClockSkewStaleIsUnknown(t *testing.T) {
	now := time.Now()
	var s clockSkew
	// Observed 10 minutes ago: it says nothing about the clock now.
	s.observe(now.Add(-10*time.Minute-time.Second), now.Add(-10*time.Minute))
	if got := s.ms(skewMaxAge); got != 0 {
		t.Errorf("stale ms() = %d, want 0", got)
	}
}

func TestClockSkewZeroValueIsUnknown(t *testing.T) {
	var s clockSkew
	if got := s.ms(skewMaxAge); got != 0 {
		t.Errorf("zero-value ms() = %d, want 0", got)
	}
}

func TestClientClockSkewWithoutObservation(t *testing.T) {
	c := NewClient("test-project")
	if got := c.ClockSkewMS(); got != 0 {
		t.Errorf("ClockSkewMS() before any response = %d, want 0", got)
	}
}
