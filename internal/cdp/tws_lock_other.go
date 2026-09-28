//go:build !linux

package cdp

// excludeFromDump is a no-op off Linux: MADV_DONTDUMP has no portable
// equivalent. mlock still applies (see lockBuffer).
func excludeFromDump([]byte) {}
