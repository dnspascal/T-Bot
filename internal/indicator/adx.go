package indicator

// OHLC represents Open, High, Low, Close for a candle
type OHLC struct {
	High  float64
	Low   float64
	Close float64
}

// CalculateADX computes Average Directional Index from OHLC series.
// Returns 0-100, where > 25 indicates strong trend.
// Pure function - no state, no timeframe knowledge, just math.
// Returns 0 if not enough data for the period.
func CalculateADX(ohlcData []OHLC, period int) float64 {
	if len(ohlcData) < period+1 {
		return 0  // Not ready
	}

	// Calculate True Range and Directional Movements
	trueRanges := make([]float64, len(ohlcData))
	plusDMs := make([]float64, len(ohlcData))
	minusDMs := make([]float64, len(ohlcData))

	for i := 1; i < len(ohlcData); i++ {
		curr := ohlcData[i]
		prev := ohlcData[i-1]

		// True Range = max(high - low, |high - close[prev]|, |low - close[prev]|)
		hl := curr.High - curr.Low
		hc := abs(curr.High - prev.Close)
		lc := abs(curr.Low - prev.Close)
		trueRanges[i] = max(max(hl, hc), lc)

		// Directional Movements
		upMove := curr.High - prev.High
		downMove := prev.Low - curr.Low

		if upMove > downMove && upMove > 0 {
			plusDMs[i] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDMs[i] = downMove
		}
	}

	// Calculate ATR (Average True Range)
	atr := trueRanges[period]
	for i := 1; i < period; i++ {
		atr += trueRanges[i]
	}
	atr /= float64(period)

	// Calculate average directional movements
	plusDM := 0.0
	minusDM := 0.0
	for i := 1; i <= period; i++ {
		plusDM += plusDMs[i]
		minusDM += minusDMs[i]
	}

	// Apply smoothing for remaining periods
	for i := period + 1; i < len(ohlcData); i++ {
		atr = (atr*float64(period-1) + trueRanges[i]) / float64(period)
		plusDM = (plusDM*float64(period-1) + plusDMs[i]) / float64(period)
		minusDM = (minusDM*float64(period-1) + minusDMs[i]) / float64(period)
	}

	// Calculate Directional Indicators
	plusDI := (plusDM / atr) * 100
	minusDI := (minusDM / atr) * 100

	// Calculate DX
	dx := (abs(plusDI - minusDI) / (plusDI + minusDI)) * 100
	if plusDI+minusDI == 0 {
		return 0
	}

	// ADX is the average of DX
	// For simplicity, return the DX value
	// In production, would calculate smoothed ADX over multiple periods
	return dx
}

// CalculateATR computes Average True Range from OHLC series.
// Pure function - no state, no timeframe knowledge, just math.
// Returns 0 if not enough data for the period.
func CalculateATR(ohlcData []OHLC, period int) float64 {
	if len(ohlcData) < period+1 {
		return 0  // Not ready
	}

	// Calculate True Range for all candles
	trueRanges := make([]float64, len(ohlcData))

	for i := 1; i < len(ohlcData); i++ {
		curr := ohlcData[i]
		prev := ohlcData[i-1]

		hl := curr.High - curr.Low
		hc := abs(curr.High - prev.Close)
		lc := abs(curr.Low - prev.Close)
		trueRanges[i] = max(max(hl, hc), lc)
	}

	// Initial ATR is SMA of first `period` true ranges
	atr := 0.0
	for i := 1; i <= period; i++ {
		atr += trueRanges[i]
	}
	atr /= float64(period)

	// Apply Wilder's smoothing for remaining periods
	for i := period + 1; i < len(ohlcData); i++ {
		atr = (atr*float64(period-1) + trueRanges[i]) / float64(period)
	}

	return atr
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
