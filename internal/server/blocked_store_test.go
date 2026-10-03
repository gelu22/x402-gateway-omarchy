package server

import (
	"fmt"
	"testing"
	"time"

	"gateway/internal/gateway"
)

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// TestBlockedStoreRingEvictsOldest (49.2): the list is bounded; a burst of
// refusals cannot grow it without limit.
func TestBlockedStoreRingEvictsOldest(t *testing.T) {
	b := newBlockedStore(fixedClock(time.Unix(1000, 0)))
	for i := 0; i < blockedMax+5; i++ {
		b.Record("GET", fmt.Sprintf("https://s%d.example/x", i), nil, nil, 1_000, "mfa_required")
	}
	got := b.List()
	if len(got) != blockedMax {
		t.Fatalf("len = %d, want %d", len(got), blockedMax)
	}
	// Oldest evicted: the first kept entry is the 6th recorded (s5).
	if got[0].URL != "https://s5.example/x" {
		t.Fatalf("oldest kept = %s, want s5 (oldest evicted)", got[0].URL)
	}
}

// TestBlockedStoreTTLDropsStale (49.2): an entry the owner ignored past the TTL
// is gone, so a stale price is not approved by reflex.
func TestBlockedStoreTTLDropsStale(t *testing.T) {
	now := time.Unix(1000, 0)
	b := newBlockedStore(func() time.Time { return now })
	b.Record("GET", "https://a.example/x", nil, nil, 1_000, "mfa_required")
	now = now.Add(blockedTTL + time.Second)
	if got := b.List(); len(got) != 0 {
		t.Fatalf("stale entry survived: %+v", got)
	}
}

// TestBlockedStoreHeadersOnlyForBodyMethods (49.2): a GET carries no request
// body, so no headers are kept; a POST keeps them (needed to replay).
func TestBlockedStoreHeadersOnlyForBodyMethods(t *testing.T) {
	b := newBlockedStore(fixedClock(time.Unix(1000, 0)))
	h := map[string]string{"Authorization": "secret"}
	get, _ := b.Record("GET", "https://a.example/x", nil, h, 1_000, "mfa_required")
	if get.Headers != nil {
		t.Fatalf("GET must not remember headers, got %v", get.Headers)
	}
	post, _ := b.Record("POST", "https://a.example/x", []byte("{}"), h, 1_000, "unknown_seller")
	if post.Headers["Authorization"] != "secret" {
		t.Fatalf("POST headers must be remembered for replay, got %v", post.Headers)
	}
	// The stored header map must be a copy, not the caller's.
	h["Authorization"] = "changed"
	if post.Headers["Authorization"] != "secret" {
		t.Fatal("stored headers must be a copy")
	}
}

// TestBlockedStoreNotifyCoalesced (49.2): at most one notification per window,
// so a burst of refused agent calls cannot flood the desktop.
func TestBlockedStoreNotifyCoalesced(t *testing.T) {
	now := time.Unix(1000, 0)
	b := newBlockedStore(func() time.Time { return now })
	if _, notify := b.Record("GET", "https://a/x", nil, nil, 1, "mfa_required"); !notify {
		t.Fatal("first refusal must notify")
	}
	if _, notify := b.Record("GET", "https://b/x", nil, nil, 1, "mfa_required"); notify {
		t.Fatal("second refusal inside the window must not notify")
	}
	now = now.Add(blockedNotifyWindow + time.Second)
	if _, notify := b.Record("GET", "https://c/x", nil, nil, 1, "mfa_required"); !notify {
		t.Fatal("after the window a refusal must notify again")
	}
}

// TestBlockedStoreGetRemove (49.2): the owner acts on a specific entry, then it
// is gone.
func TestBlockedStoreGetRemove(t *testing.T) {
	b := newBlockedStore(fixedClock(time.Unix(1000, 0)))
	br, _ := b.Record("GET", "https://a/x", nil, nil, 5_000, "mfa_required")
	if _, ok := b.Get(br.ID); !ok {
		t.Fatal("Get must find the recorded entry")
	}
	if !b.Remove(br.ID) {
		t.Fatal("Remove must report success")
	}
	if _, ok := b.Get(br.ID); ok {
		t.Fatal("removed entry must be gone")
	}
	if b.Remove("nope") {
		t.Fatal("removing an unknown id must be false")
	}
}

// TestRecordIfBlockedClassifies (49.2): only "needs the owner" refusals are
// listed — a CDP MFA request or an overridable denial. Everything else is not
// the owner's queue.
func TestRecordIfBlockedClassifies(t *testing.T) {
	t.Setenv("GATEWAY_NOTIFY", "0")
	srv := &Server{blocked: newBlockedStore(fixedClock(time.Unix(1000, 0)))}

	srv.recordIfBlocked("GET", "https://mfa/x", nil, nil, &gateway.PolicyError{Code: "mfa_required", AmountMicro: 10})
	srv.recordIfBlocked("GET", "https://cap/x", nil, nil, &gateway.PolicyError{Code: "domain_cap_exceeded", AmountMicro: 20, CanOverride: true})
	srv.recordIfBlocked("GET", "https://hard/x", nil, nil, &gateway.PolicyError{Code: "policy_violation", AmountMicro: 30, CanOverride: false})
	srv.recordIfBlocked("GET", "https://plain/x", nil, nil, fmt.Errorf("some other error"))

	got := srv.blocked.List()
	if len(got) != 2 {
		t.Fatalf("listed %d, want 2 (mfa_required + overridable)", len(got))
	}
	if got[0].URL != "https://mfa/x" || got[1].URL != "https://cap/x" {
		t.Fatalf("wrong entries listed: %+v", got)
	}
}

// TestRecordBlockedNilStoreSafe (49.2): a zero-value Server (tests) must not
// panic when a payment is refused.
func TestRecordBlockedNilStoreSafe(t *testing.T) {
	srv := &Server{}
	srv.recordIfBlocked("GET", "https://x/y", nil, nil, &gateway.PolicyError{Code: "mfa_required"})
}

// TestBlockedSummariesOmitBodyAndHeaders (49.2): /status shows what waits, but
// never the blocked request's body or the agent's headers.
func TestBlockedSummariesOmitBodyAndHeaders(t *testing.T) {
	b := newBlockedStore(fixedClock(time.Unix(1000, 0)))
	b.Record("POST", "https://a.example/x", []byte("secret-body"), map[string]string{"Authorization": "secret"}, 4_200, "unknown_seller")
	got := b.Summaries()
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].URL != "https://a.example/x" || got[0].AmountMicro != 4_200 || got[0].Method != "POST" || got[0].Reason != "unknown_seller" {
		t.Fatalf("summary fields wrong: %+v", got[0])
	}
	if got[0].ID == "" || got[0].At == "" {
		t.Fatalf("summary must carry id and timestamp: %+v", got[0])
	}
	// The summary type has no body/header fields at all — a compile-time pin.
	// This assertion documents the intent; the struct definition enforces it.
	full, ok := b.Get(got[0].ID)
	if !ok || string(full.Body) != "secret-body" {
		t.Fatalf("full request is still available server-side for replay: %+v", full)
	}
}
