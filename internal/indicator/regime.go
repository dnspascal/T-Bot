package indicator

func CalculateRegime(emaFast, emaSlow, adx, high, low float64, ohlc []OHLC) string {
	if adx < 25 {
		return "ranging" // Weak trend
	}

	if emaFast > emaSlow && adx >= 25 {
		return "trending_up"
	}
	if emaFast < emaSlow && adx >= 25 {
		return "trending_down"
	}

	if len(ohlc) > 20 {
		recentHigh := ohlc[len(ohlc)-20].High
		recentLow := ohlc[len(ohlc)-20].Low
		for i := len(ohlc) - 19; i < len(ohlc); i++ {
			if ohlc[i].High > recentHigh {
				recentHigh = ohlc[i].High
			}
			if ohlc[i].Low < recentLow {
				recentLow = ohlc[i].Low
			}
		}
		if high > recentHigh || low < recentLow {
			return "breakout"
		}
	}

	return "ranging"
}

// CalculateVolatilityTrend determines if volatility is expanding, contracting, or stable.
// prevATR is the ATR value from the previous candle.
func CalculateVolatilityTrend(currentATR, prevATR float64) string {
	if prevATR == 0 {
		return "stable"
	}
	atrChange := ((currentATR - prevATR) / prevATR) * 100
	if atrChange > 2 {
		return "expanding"
	}
	if atrChange < -2 {
		return "contracting"
	}
	return "stable"
}

// CalculateMomentumDirection determines if momentum is rising, falling, or stable
func CalculateMomentumDirection(rsi float64, closes []float64) string {
	if len(closes) < 2 {
		return "stable"
	}

	if rsi > 60 {
		return "rising"
	}
	if rsi < 40 {
		return "falling"
	}
	return "stable"
}

// CalculateVolumeMA calculates the simple moving average of volume over the last period candles.
func CalculateVolumeMA(volumes []int64, period int) int64 {
	if len(volumes) == 0 {
		return 0
	}
	if len(volumes) < period {
		period = len(volumes)
	}

	var sum int64
	for i := len(volumes) - period; i < len(volumes); i++ {
		sum += volumes[i]
	}

	return sum / int64(period)
}
