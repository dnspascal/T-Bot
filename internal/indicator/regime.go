package indicator

import (
	"math"

	"github.com/denismgaya/t-bot/internal/config"
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
			return config.Breakout
		}
	}

	if emaFast == 0 || emaSlow == 0 {
		return config.Ranging
	}

	gap := math.Abs(emaFast-emaSlow) / ((emaFast + emaSlow) / 2)
	if gap < 0.001 {
		return config.Ranging
	}

	if emaFast > emaSlow {
		return config.TrendingUp
	}

	return config.TrendingDown
}

func CalculateVolatilityTrend(currentATR, prevATR float64) string {
	if prevATR == 0 {
		return config.VolatilityStable
	}
	atrChange := ((currentATR - prevATR) / prevATR) * 100
	if atrChange > 2 {
		return config.VolatilityExpanding
	}
	if atrChange < -2 {
		return config.VolatilityContracting
	}
	return config.VolatilityStable
}

func CalculateMomentumDirection(rsi float64, closes []float64) string {
	if len(closes) < 4 {
		return config.MomentumStable
	}

	recent := closes[len(closes)-1]
	prior := closes[len(closes)-4]
	priceRising := recent > prior
	priceFalling := recent < prior

	switch {
	case rsi > 60 && priceRising:
		return config.MomentumRising
	case rsi < 40 && priceFalling:
		return config.MomentumFalling
	default:
		return config.MomentumStable
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
