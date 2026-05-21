package strategy

import "time"

// Candle is an OHLC price bar for a fixed time period.
type Candle struct {
	Open     float64
	High     float64
	Low      float64
	Close    float64
	OpenTime time.Time
}

// CandleAggregator builds OHLC candles from individual price ticks.
// When the time period rolls over, the completed candle is emitted.
type CandleAggregator struct {
	period  time.Duration
	current *Candle
}

func NewCandleAggregator(period time.Duration) *CandleAggregator {
	return &CandleAggregator{period: period}
}

// AddTick feeds a price tick. Returns the completed candle and true when the
// current period ends and a new one begins. Returns false on all other ticks.
func (a *CandleAggregator) AddTick(price float64, t time.Time) (Candle, bool) {
	periodStart := t.UTC().Truncate(a.period)

	if a.current == nil {
		a.current = &Candle{
			Open: price, High: price, Low: price, Close: price,
			OpenTime: periodStart,
		}
		return Candle{}, false
	}

	if periodStart.Equal(a.current.OpenTime) {
		if price > a.current.High {
			a.current.High = price
		}
		if price < a.current.Low {
			a.current.Low = price
		}
		a.current.Close = price
		return Candle{}, false
	}

	// Period rolled over — emit completed candle and start the new one
	completed := *a.current
	a.current = &Candle{
		Open: price, High: price, Low: price, Close: price,
		OpenTime: periodStart,
	}
	return completed, true
}
