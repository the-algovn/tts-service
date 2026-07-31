// Package pricing prices a synthesis at provider list rates.
//
// Prices verified against https://cloud.google.com/text-to-speech/pricing on
// 2026-07-31. CostUSD returns the LIST-price marginal cost: it deliberately
// does NOT subtract the monthly free allowance, because that allowance is a
// per-billing-account monthly quantity and this service is stateless. Callers
// get the allowance from FreeTierChars and interpret their ledger with it.
package pricing

import "strings"

// per1M is the list price in USD per one million characters, by tier.
var per1M = map[string]float64{
	"chirp3-hd": 30.0,
	"neural2":   16.0,
	"wavenet":   4.0,
	"standard":  4.0,
}

// freeChars is the monthly free allowance per tier, in characters.
var freeChars = map[string]int64{
	"chirp3-hd": 1_000_000,
	"neural2":   0,
	"wavenet":   4_000_000,
	"standard":  4_000_000,
}

// TierOf derives the billing tier from a Google voice name. Unknown names bill
// as standard, the cheapest tier -- an unknown voice is far more likely to be a
// typo than a new premium tier, and this keeps a mistake from inventing spend.
func TierOf(voiceName string) string {
	switch {
	case strings.Contains(voiceName, "Chirp3"):
		return "chirp3-hd"
	case strings.Contains(voiceName, "Neural2"):
		return "neural2"
	case strings.Contains(voiceName, "Wavenet"):
		return "wavenet"
	default:
		return "standard"
	}
}

// paid reports whether a provider bills per character at all.
func paid(provider string) bool { return provider == "google" }

func CostUSD(provider, voiceName string, chars int) float64 {
	if !paid(provider) {
		return 0
	}
	return per1M[TierOf(voiceName)] / 1e6 * float64(chars)
}

func FreeTierChars(provider, voiceName string) int64 {
	if !paid(provider) {
		return 0
	}
	return freeChars[TierOf(voiceName)]
}
