package indicator

// CalculateEMA computes exponential moving average from price series.
// Pure function - no state, no timeframe knowledge, just math.
// Returns 0 if not enough prices for the period.
func CalculateEMA(prices []float64, period int) float64 {
	if len(prices) < period {
		return 0
	}

	k := 2.0 / float64(period+1)

	// Initial SMA from oldest `period` prices
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += prices[i]
	}
	ema := sum / float64(period)

	// Apply EMA smoothing from position `period` onwards
	for i := period; i < len(prices); i++ {
		ema = prices[i]*k + ema*(1-k)
	}

	return ema
}
