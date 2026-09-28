package session

import (
	"context"
	"testing"
	"time"
)

// 34.3: tick() is the loop-driven decision layer of the session lifecycle
// (proactive access-token refresh + TWS renewal). It had 0% coverage, so a
// regression in "when to refresh" would go unnoticed until tokens expired.

func TestTickRefreshesWhenAccessTokenExpiresSoon(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, err := New(f.client(), NewStore(dir), slogNop())
	if err != nil {
		t.Fatal(err)
	}
	m.SetNow(fixedNow(0))
	if _, err := m.AccessToken(); err != nil { // resume: refresh #1, expiry +15 min
		t.Fatalf("resume refresh: %v", err)
	}
	if got := f.refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}

	// now+refreshAhead (3 min) crosses the +15 min expiry → tick must refresh.
	m.SetNow(fixedNow(13 * time.Minute))
	m.tick()
	if got := f.refreshCalls.Load(); got != 2 {
		t.Fatalf("refresh calls after tick = %d, want 2", got)
	}
	if s, _, _ := m.Status(); s != StateActive {
		t.Fatalf("state = %s, want active", s)
	}
}

func TestTickRenewsTWSWhenExpiringSoon(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	if _, err := m.WalletSecret(); err != nil { // bootstrap TWS, valid +15 min
		t.Fatalf("bootstrap TWS: %v", err)
	}
	before := f.twsCalls.Load()

	// twsAhead is 2 min → past +13 min the renewal window is open.
	m.SetNow(fixedNow(14 * time.Minute))
	m.tick()
	if got := f.twsCalls.Load(); got <= before {
		t.Fatalf("tws registrations after tick = %d, want > %d", got, before)
	}
	if ids, uniform := f.twsIdentities(); ids != 1 || !uniform {
		t.Fatalf("tick must renew the same TWS identity, got ids=%d uniform=%v", ids, uniform)
	}
}

func TestTickNoSessionIsNoop(t *testing.T) {
	dir := t.TempDir() // no seedStore: logged out
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	m.tick()

	if got := f.refreshCalls.Load(); got != 0 {
		t.Fatalf("refresh calls = %d, want 0 without a session", got)
	}
	if got := f.twsCalls.Load(); got != 0 {
		t.Fatalf("tws calls = %d, want 0 without a session", got)
	}
}

func TestTickRefreshFailureKeepsStateAndDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	m.SetNow(fixedNow(0))
	if _, err := m.AccessToken(); err != nil {
		t.Fatalf("resume refresh: %v", err)
	}

	f.failRefresh.Store(true)
	m.SetNow(fixedNow(13 * time.Minute))
	m.tick() // must log and return, not panic or retry-storm

	if got := f.refreshCalls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1 (one attempt, no retry storm)", got)
	}
	if s, _, _ := m.Status(); s != StateActive {
		t.Fatalf("transient failure must keep the session active, got %s", s)
	}
}

func TestRunReturnsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	seedStore(t, dir)
	f := newFakeCDP(t)
	f.registerRoutes(fixedNow(15 * time.Minute)())

	m, _ := New(f.client(), NewStore(dir), slogNop())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.Run(ctx)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel (goroutine leak)")
	}
}
