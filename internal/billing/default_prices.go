package billing

import "github.com/router-for-me/CLIProxyAPI/v6/internal/policy"

// DefaultPrices provides a built-in fallback table when no saved override exists.
// Keys are normalised via policy.NormaliseModelKey.
var DefaultPrices = map[string]PriceMicroUSDPer1M{
	// Anthropic
	policy.NormaliseModelKey("claude-opus-4-5-20251101"): {
		Prompt:     5_000_000,  // $5.00 / 1M
		Completion: 25_000_000, // $25.00 / 1M
		Cached:     500_000,    // $0.50 / 1M
	},
}
