package binance

import (
	"github.com/denismgaya/t-bot/internal/indicator"
)

type IndicatorState struct {
	Timeframe string
	EMA9      *indicator.EMA
	EMA21     *indicator.EMA
	RSI       *indicator.RSI
	ATR       *indicator.ATR
	ADX       *indicator.ADX
}

func NewIndicatorState(timeframe string) *IndicatorState {
	return &IndicatorState{
		Timeframe: timeframe,
		EMA9:      indicator.NewEMA(9),
		EMA21:     indicator.NewEMA(21),
		RSI:       indicator.NewRSI(14),
		ATR:       indicator.NewATR(14),
		ADX:       indicator.NewADX(14),
	}
}

func (is *IndicatorState) AddCandle(open, high, low, close, volume float64) {
	is.EMA9.Add(close)
	is.EMA21.Add(close)
	is.RSI.Add(close)
	is.ATR.Add(high, low, close)
	is.ADX.Add(high, low, close)
}
