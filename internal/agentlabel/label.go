// Package agentlabel is the SSOT for the client-declared agent label.
// The label is a declaration, not an identity: any same-uid process can
// set GATEWAY_AGENT or omit it (THREAT-MODEL T2).
package agentlabel

import (
	"context"
	"regexp"
)

const (
	Env    = "GATEWAY_AGENT"
	Header = "X-Gateway-Agent"
	MaxLen = 32
)

var validRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

type ctxKey struct{}

// Valid reports whether s is a well-formed non-empty label.
// Empty string is handled separately by callers (legal = no header).
func Valid(s string) bool {
	if s == "" || len(s) > MaxLen {
		return false
	}
	return validRE.MatchString(s)
}

// With stores label in ctx for daemon handlers (54.3+).
func With(ctx context.Context, label string) context.Context {
	return context.WithValue(ctx, ctxKey{}, label)
}

// From returns the label from ctx, or "" if unset.
func From(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}
