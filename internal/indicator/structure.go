package indicator

// CalculateSupportResistance calculates key price levels (support, resistance, trend extremes)
// Returns: support, resistance, trendHigh, trendLow
func CalculateSupportResistance(ohlc []OHLC) (support, resistance, trendHigh, trendLow float64) {
	if len(ohlc) == 0 {
		return 0, 0, 0, 0
	}

	// Trend high: highest high in last 20 candles
	// Trend low: lowest low in last 20 candles
	lookback := 20
	if len(ohlc) < lookback {
		lookback = len(ohlc)
	}

	trendHigh = ohlc[len(ohlc)-lookback].High
	trendLow = ohlc[len(ohlc)-lookback].Low

	for i := len(ohlc) - lookback; i < len(ohlc); i++ {
		if ohlc[i].High > trendHigh {
			trendHigh = ohlc[i].High
		}
		if ohlc[i].Low < trendLow {
			trendLow = ohlc[i].Low
		}
	}

	// Resistance = trend high, Support = trend low
	resistance = trendHigh
	support = trendLow

	return support, resistance, trendHigh, trendLow
}

// CalculateBreakoutLevel determines if price broke through previous high/low
// Returns the level that was broken, or 0 if no breakout
func CalculateBreakoutLevel(high, low float64, ohlc []OHLC) float64 {
	if len(ohlc) < 2 {
		return 0
	}

	prevHigh := ohlc[len(ohlc)-2].High
	prevLow := ohlc[len(ohlc)-2].Low

	if high > prevHigh {
		return prevHigh // Broke through previous resistance
	}
	if low < prevLow {
		return prevLow // Broke through previous support
	}

	return 0 // No breakout
}
