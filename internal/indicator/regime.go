package indicator

const (
    TrendingUp   = "trending_up"
    TrendingDown = "trending_down"
    Ranging      = "ranging"
    Breakout     = "breakout"
)

func CalculateRegime(emaFast, emaSlow, adx, high, low float64, ohlc []OHLC) string {
	if len(ohlc) > 20 {
		prior := ohlc[:len(ohlc)-1]
		start := max(len(prior)-20, 0)
		refHigh := prior[start].High
		refLow := prior[start].Low
		for _, c := range prior[start+1:] {
			if c.High > refHigh {
				refHigh = c.High
			}
			if c.Low < refLow {
				refLow = c.Low
			}
		}
		if high > refHigh || low < refLow {
			return "breakout"
		}
	}

	if adx < 25 {
		return "ranging"
	}
	if emaFast > emaSlow {
		return "trending_up"
	}
	if emaFast < emaSlow {
		return "trending_down"
	}
	return "ranging"
}

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

// CalculateMomentumDirection determines if momentum is rising, falling, or stable.
// Both RSI and the 3-bar price slope must agree to avoid false signals.
func CalculateMomentumDirection(rsi float64, closes []float64) string {
	if len(closes) < 4 {
		return "stable"
	}

	// 3-bar slope: compare the last close to the close 3 bars ago.
	recent := closes[len(closes)-1]
	prior := closes[len(closes)-4]
	priceRising := recent > prior
	priceFalling := recent < prior

	switch {
	case rsi > 60 && priceRising:
		return "rising"
	case rsi < 40 && priceFalling:
		return "falling"
	default:
		return "stable"
	}
}

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
