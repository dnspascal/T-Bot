package strategy

import "time"

// Confluence measures how many independent signals agree on the trade direction.
type Confluence int

const (
	ConfluenceWeak   Confluence = 1 // EMA crossover confirmed, RSI not yet above/below midline
	ConfluenceStrong Confluence = 2 // EMA crossover + RSI confirms trend direction
)

// Decision is the output produced each time an M5 candle closes.
type Decision struct {
	Signal     Signal
	Confluence Confluence
	FastEMA    float64
	SlowEMA    float64
	RSI        float64
	InSession  bool
	Candle     Candle
}

// CombinedStrategy runs EMA 9/21 crossover and RSI 14 on M5 candles.
//
// Rules:
//  1. Session gate  — no trades outside 08:00–17:00 UTC weekdays
//  2. EMA crossover — fast crosses slow = primary entry trigger
//  3. RSI filter    — skip if RSI > 65 on BUY or < 35 on SELL (exhausted moves)
//  4. Confluence    — RSI above 50 on BUY / below 50 on SELL = ConfluenceStrong
type CombinedStrategy struct {
	ema *EMAStrategy
	rsi *RSI
	agg *CandleAggregator
}

func NewCombinedStrategy(fastPeriod, slowPeriod, rsiPeriod int) *CombinedStrategy {
	return &CombinedStrategy{
		ema: NewEMAStrategy(fastPeriod, slowPeriod),
		rsi: NewRSI(rsiPeriod),
		agg: NewCandleAggregator(5 * time.Minute),
	}
}

// WarmUp pre-loads historical M5 close prices so EMA and RSI start with
// meaningful values instead of needing 35+ live candles to initialise.
func (s *CombinedStrategy) WarmUp(closePrices []float64) {
	for _, p := range closePrices {
		s.ema.AddPrice(p)
		s.rsi.AddPrice(p)
	}
}

// AddCandle feeds a completed M5 candle from a cTrader trendbar event directly.
// Use this instead of AddTick when subscribed to live trendbars.
func (s *CombinedStrategy) AddCandle(c Candle) Decision {
	return s.evaluate(c)
}

// AddTick feeds a price tick. Returns a Decision and true only when an M5
// candle completes. On all other ticks returns false — no action needed.
func (s *CombinedStrategy) AddTick(price float64, t time.Time) (Decision, bool) {
	c, fired := s.agg.AddTick(price, t)
	if !fired {
		return Decision{}, false
	}
	return s.evaluate(c), true
}

// evaluate runs EMA + RSI on a completed candle and produces a Decision.
func (s *CombinedStrategy) evaluate(c Candle) Decision {
	emaSig := s.ema.AddPrice(c.Close)
	rsiVal := s.rsi.AddPrice(c.Close)

	dec := Decision{
		FastEMA:   s.ema.FastEMA(),
		SlowEMA:   s.ema.SlowEMA(),
		RSI:       rsiVal,
		InSession: isTradingSession(c.OpenTime),
		Candle:    c,
	}

	if !dec.InSession {
		dec.Signal = Hold
		return dec
	}
	if emaSig == Hold {
		dec.Signal = Hold
		return dec
	}
	if !s.rsi.IsReady() {
		dec.Signal = Hold
		return dec
	}
	if emaSig == Buy && rsiVal > 65 {
		dec.Signal = Hold
		return dec
	}
	if emaSig == Sell && rsiVal < 35 {
		dec.Signal = Hold
		return dec
	}

	dec.Signal = emaSig
	if (emaSig == Buy && rsiVal > 50) || (emaSig == Sell && rsiVal < 50) {
		dec.Confluence = ConfluenceStrong
	} else {
		dec.Confluence = ConfluenceWeak
	}
	return dec
}

// isTradingSession returns true during London + New York hours (08:00–22:00 UTC) on weekdays.
func isTradingSession(t time.Time) bool {
	utc := t.UTC()
	if utc.Weekday() == time.Saturday || utc.Weekday() == time.Sunday {
		return false
	}
	h := utc.Hour()
	return h >= 8 && h < 22
}
