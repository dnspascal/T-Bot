package indicator

import "github.com/denismgaya/t-bot/internal/util"

type OHLC struct {
	High  float64
	Low   float64
	Close float64
}


func CalculateADX(ohlcData []OHLC, period int) float64 {
	if len(ohlcData) < period+1 {
		return 0  
	}

	trueRanges := make([]float64, len(ohlcData))
	plusDMs := make([]float64, len(ohlcData))
	minusDMs := make([]float64, len(ohlcData))

	for i := 1; i < len(ohlcData); i++ {
		curr := ohlcData[i]
		prev := ohlcData[i-1]

		hl := curr.High - curr.Low
		hc := util.Abs(curr.High - prev.Close)
		lc := util.Abs(curr.Low - prev.Close)
		trueRanges[i] = util.Max(util.Max(hl, hc), lc)

		upMove := curr.High - prev.High
		downMove := prev.Low - curr.Low

		if upMove > downMove && upMove > 0 {
			plusDMs[i] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDMs[i] = downMove
		}
	}

	atr := trueRanges[period]
	for i := 1; i < period; i++ {
		atr += trueRanges[i]
	}
	atr /= float64(period)

	plusDM := 0.0
	minusDM := 0.0
	for i := 1; i <= period; i++ {
		plusDM += plusDMs[i]
		minusDM += minusDMs[i]
	}

	plusDI := (plusDM / atr) * 100
	minusDI := (minusDM / atr) * 100
	dx := (util.Abs(plusDI - minusDI) / (plusDI + minusDI)) * 100
	if plusDI+minusDI == 0 {
		return 0
	}

	adx := dx

	for i := period + 1; i < len(ohlcData); i++ {
		atr = (atr*float64(period-1) + trueRanges[i]) / float64(period)
		plusDM = (plusDM*float64(period-1) + plusDMs[i]) / float64(period)
		minusDM = (minusDM*float64(period-1) + minusDMs[i]) / float64(period)

		plusDI = (plusDM / atr) * 100
		minusDI = (minusDM / atr) * 100
		dx = (util.Abs(plusDI - minusDI) / (plusDI + minusDI)) * 100

		adx = (adx*float64(period-1) + dx) / float64(period)
	}

	return adx
}
