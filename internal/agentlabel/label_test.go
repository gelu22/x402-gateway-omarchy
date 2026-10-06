package agentlabel

import (
	"context"
	"testing"
)

func TestValid(t *testing.T) {
	ok := []string{"opencode", "a", "claude.code", "x_1-2", "abcdefghijklmnopqrstuvwxyz012345"}
	for _, s := range ok {
		if !Valid(s) {
			t.Errorf("Valid(%q) = false, want true", s)
		}
	}
	bad := []string{"", "A", "a b", "ą", "abcdefghijklmnopqrstuvwxyz0123456", "../x", "x\n", "-bad", ".bad", "_bad", "HasCaps"}
	for _, s := range bad {
		if Valid(s) {
			t.Errorf("Valid(%q) = true, want false", s)
		}
	}
}

func TestWithFrom(t *testing.T) {
	ctx := With(context.Background(), "opencode")
	if got := From(ctx); got != "opencode" {
		t.Fatalf("From = %q, want opencode", got)
	}
	if got := From(context.Background()); got != "" {
		t.Fatalf("From(Background) = %q, want empty", got)
	}
	if got := From(nil); got != "" {
		t.Fatalf("From(nil) = %q, want empty", got)
	}
}
