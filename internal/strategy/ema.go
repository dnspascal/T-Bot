package strategy

// EMA calculates exponential moving averages on a price series
// and generates BUY / SELL / HOLD signals.
//
// Logic:
//   Fast EMA (9) crosses above Slow EMA (21) → BUY
//   Fast EMA (9) crosses below Slow EMA (21) → SELL
//   No cross → HOLD

type Signal int

const (
	Hold Signal = iota
	Buy
	Sell
)

func (s Signal) String() string {
	switch s {
	case Buy:
		return "BUY"
	case Sell:
		return "SELL"
	default:
		return "HOLD"
	}
}

type EMAStrategy struct {
	fastPeriod int
	slowPeriod int
	prices     []float64

	fastEMA float64
	slowEMA float64
	prevFast float64
	prevSlow float64
	warmedUp bool
}

func NewEMAStrategy(fastPeriod, slowPeriod int) *EMAStrategy {
	return &EMAStrategy{
		fastPeriod: fastPeriod,
		slowPeriod: slowPeriod,
	}
}

// AddPrice feeds a new mid price into the strategy and returns a signal.
// Prices must be added one at a time in chronological order.
func (s *EMAStrategy) AddPrice(price float64) Signal {
	s.prices = append(s.prices, price)

	if len(s.prices) < s.slowPeriod {
		return Hold
	}

	// First time we have enough data — seed EMAs with SMA then drop the slice
	if !s.warmedUp {
		s.fastEMA = sma(s.prices[len(s.prices)-s.fastPeriod:])
		s.slowEMA = sma(s.prices)
		s.prevFast = s.fastEMA
		s.prevSlow = s.slowEMA
		s.warmedUp = true
		s.prices = nil // no longer needed — EMA is now self-updating
		return Hold
	}

	s.prevFast = s.fastEMA
	s.prevSlow = s.slowEMA

	kFast := 2.0 / float64(s.fastPeriod+1)
	kSlow := 2.0 / float64(s.slowPeriod+1)
	s.fastEMA = price*kFast + s.fastEMA*(1-kFast)
	s.slowEMA = price*kSlow + s.slowEMA*(1-kSlow)

	// Detect crossover
	wasBullish := s.prevFast > s.prevSlow
	isBullish := s.fastEMA > s.slowEMA

	if !wasBullish && isBullish {
		return Buy
	}
	if wasBullish && !isBullish {
		return Sell
	}
	return Hold
}

// FastEMA returns the current fast EMA value (for logging/display)
func (s *EMAStrategy) FastEMA() float64 { return s.fastEMA }

// SlowEMA returns the current slow EMA value (for logging/display)
func (s *EMAStrategy) SlowEMA() float64 { return s.slowEMA }

func sma(prices []float64) float64 {
	sum := 0.0
	for _, p := range prices {
		sum += p
	}
	return sum / float64(len(prices))
}
