package billing

import (
	"math"
)

const tokensPerMillion = int64(1_000_000)

func usdPer1MToMicroUSDPer1M(v float64) int64 {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	// v USD => v * 1e6 micro-USD
	return int64(math.Round(v * 1_000_000))
}

func USDPer1MToMicroUSDPer1M(v float64) int64 { return usdPer1MToMicroUSDPer1M(v) }

func microUSDPer1MToUSDPer1M(v int64) float64 {
	if v <= 0 {
		return 0
	}
	return float64(v) / 1_000_000
}

func MicroUSDPer1MToUSDPer1M(v int64) float64 { return microUSDPer1MToUSDPer1M(v) }

func microUSDToUSD(v int64) float64 {
	if v == 0 {
		return 0
	}
	return float64(v) / 1_000_000
}

func MicroUSDToUSD(v int64) float64 { return microUSDToUSD(v) }

func costMicroUSD(tokens int64, microUSDPer1M int64) int64 {
	if tokens <= 0 || microUSDPer1M <= 0 {
		return 0
	}
	// Round to nearest micro-USD at the end.
	return (tokens*microUSDPer1M + tokensPerMillion/2) / tokensPerMillion
}
