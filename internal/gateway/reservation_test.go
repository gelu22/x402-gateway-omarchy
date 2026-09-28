package gateway

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

// 31.3: release(0) must not free a key that markSigned already settled — that
// would let the same payment happen twice. Unreachable from reserve today
// (tokens start at 1); pinned so a future caller cannot regress the money path.
func TestReleaseZeroKeepsSettledMark(t *testing.T) {
	gw, _ := newGateway(t, 5_000_000, 250_000)
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	gw.now = func() time.Time { return base }

	key := "GET https://seller.example/z"
	gw.markSigned(key)

	gw.release(key, 0)

	if _, err := gw.reserve(key); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("release(0) freed a settled key: reserve returned %v, want ErrDuplicate", err)
	}
}

// 36.3: the dedup map must not grow with the daemon's lifetime. Expired marks
// are swept once the map passes maxSignMarks; an in-window paid mark still
// blocks (no regression of 31.3/30.2b).
func TestLastSignStaysBounded(t *testing.T) {
	gw, _ := newGateway(t, 5_000_000, 250_000)
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	now := base
	gw.now = func() time.Time { return now }
	for i := 0; i < 2000; i++ {
		if _, err := gw.reserve("GET https://seller.example/" + strconv.Itoa(i)); err != nil {
			t.Fatalf("reserve %d: %v", i, err)
		}
	}
	// Past the window: the next reserve sweeps the expired bulk.
	now = base.Add(dedupWindow + time.Second)
	if _, err := gw.reserve("GET https://fresh.example/new"); err != nil {
		t.Fatalf("reserve after window: %v", err)
	}
	if n := len(gw.lastSign); n > maxSignMarks+64 {
		t.Fatalf("lastSign = %d entries, want <= %d (sweep must bound growth)", n, maxSignMarks+64)
	}
	// An in-window paid mark still blocks.
	gw.markSigned("GET https://fresh.example/paid")
	if _, err := gw.reserve("GET https://fresh.example/paid"); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("paid in-window key must stay blocked, got %v", err)
	}
}
