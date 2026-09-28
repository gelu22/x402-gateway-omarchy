// money.go — exact micro-USDC rendering for user-facing copy.
package gateway

import "strconv"

// formatMicroUSDC renders micro-USDC without the float rounding that turns
// sub-cent x402 amounts into "0.00". Always 2 decimals, up to 6, trailing
// zeros trimmed: 5_000_000 → "5.00", 2_000 → "0.002", 1_000 → "0.001",
// 0 → "0.00". Integer math only, no drift on the money path. FormatInt handles
// the whole int64 range (MinInt64 included), so there is no unsigned conversion
// whose overflow a reader would have to reason about.
func formatMicroUSDC(micro int64) string {
	neg := micro < 0
	s := strconv.FormatInt(micro, 10)
	if neg {
		s = s[1:]
	}
	for len(s) <= 6 {
		s = "0" + s
	}
	whole, frac := s[:len(s)-6], s[len(s)-6:]
	for len(frac) > 2 && frac[len(frac)-1] == '0' {
		frac = frac[:len(frac)-1]
	}
	out := whole + "." + frac
	if neg {
		out = "-" + out
	}
	return out
}

// budgetExhaustedBody is the once-per-day budget notification copy. Cap 0
// means auto-pay is off (every payment asks for approval), not exhausted —
// saying "0.00 USDC exhausted" for a deliberately disabled budget misleads.
func budgetExhaustedBody(spendMicro, capMicro int64) string {
	if capMicro <= 0 {
		return "Auto-pay is off (daily budget 0). Every payment will ask for approval."
	}
	return "Spent today " + formatMicroUSDC(spendMicro) + " of " + formatMicroUSDC(capMicro) +
		" USDC. Agents stop buying until midnight."
}
