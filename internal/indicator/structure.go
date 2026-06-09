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

// CalculateBreakoutLevel returns the swing level that the current candle broke through,
// or 0 if there is no breakout. Uses a 10-candle lookback (excluding the current candle)
// so single noisy bars don't trigger a false breakout.
func CalculateBreakoutLevel(high, low float64, ohlc []OHLC) float64 {
	// ohlc includes the current candle at the end — exclude it from the reference window.
	prior := ohlc[:len(ohlc)-1]
	if len(prior) < 2 {
		return 0
	}

	lookback := 10
	start := max(len(prior)-lookback, 0)

	swingHigh := prior[start].High
	swingLow := prior[start].Low
	for _, c := range prior[start+1:] {
		if c.High > swingHigh {
			swingHigh = c.High
		}
		if c.Low < swingLow {
			swingLow = c.Low
		}
	}

	if high > swingHigh {
		return swingHigh
	}
	if low < swingLow {
		return swingLow
	}
	return 0
}
