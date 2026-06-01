package indicator

func CalculateEMA(prices []float64, period int) float64 {
	if len(prices) < period {
		return 0
	}

	k := 2.0 / float64(period+1)

	sum := 0.0
	for i := range period {
		sum += prices[i]
	}
	ema := sum / float64(period)

	for i := period; i < len(prices); i++ {
		ema = prices[i]*k + ema*(1-k)
	}

	return ema
}
