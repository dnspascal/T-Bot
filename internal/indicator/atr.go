package indicator

import "github.com/denismgaya/t-bot/internal/util"

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
		hc := util.Abs(curr.High - prev.Close)
		lc := util.Abs(curr.Low - prev.Close)
		trueRanges[i] = util.Max(util.Max(hl, hc), lc)
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
