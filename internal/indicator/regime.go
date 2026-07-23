package indicator

import "math"

const (
	TrendingUp   = "trending_up"
	TrendingDown = "trending_down"
	Ranging      = "ranging"
	Breakout     = "breakout"
)

const (
	VolatilityExpanding   = "expanding"
	VolatilityContracting = "contracting"
	VolatilityStable      = "stable"
)

const (
	MomentumRising  = "rising"
	MomentumFalling = "falling"
	MomentumStable  = "stable"
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
			return Breakout
		}
	}

	if emaFast == 0 || emaSlow == 0 {
		return Ranging
	}

	gap := math.Abs(emaFast-emaSlow) / ((emaFast + emaSlow) / 2)
	if gap < 0.001 {
		return Ranging
	}

	if emaFast > emaSlow {
		return TrendingUp
	}

	return TrendingDown
}

func CalculateVolatilityTrend(currentATR, prevATR float64) string {
	if prevATR == 0 {
		return VolatilityStable
	}
	atrChange := ((currentATR - prevATR) / prevATR) * 100
	if atrChange > 2 {
		return VolatilityExpanding
	}
	if atrChange < -2 {
		return VolatilityContracting
	}
	return VolatilityStable
}

func CalculateMomentumDirection(rsi float64, closes []float64) string {
	if len(closes) < 4 {
		return MomentumStable
	}

	recent := closes[len(closes)-1]
	prior := closes[len(closes)-4]
	priceRising := recent > prior
	priceFalling := recent < prior

	switch {
	case rsi > 60 && priceRising:
		return MomentumRising
	case rsi < 40 && priceFalling:
		return MomentumFalling
	default:
		return MomentumStable
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
