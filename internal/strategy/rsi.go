package strategy

// RSI calculates Wilder's Relative Strength Index on a price series.
// Returns values 0–100. Above 70 = overbought, below 30 = oversold.
type RSI struct {
	period  int
	prev    float64
	sumGain float64
	sumLoss float64
	changes int // number of price changes accumulated
	avgGain float64
	avgLoss float64
	ready   bool
	value   float64
}

func NewRSI(period int) *RSI {
	return &RSI{period: period, value: 50}
}

// AddPrice feeds a new close price and returns the current RSI (0–100).
// Returns 50 (neutral) until enough data is accumulated (period+1 prices).
func (r *RSI) AddPrice(price float64) float64 {
	if r.prev == 0 {
		r.prev = price
		return 50
	}

	change := price - r.prev
	r.prev = price

	var gain, loss float64
	if change >= 0 {
		gain = change
	} else {
		loss = -change
	}

	if !r.ready {
		r.sumGain += gain
		r.sumLoss += loss
		r.changes++
		if r.changes == r.period {
			r.avgGain = r.sumGain / float64(r.period)
			r.avgLoss = r.sumLoss / float64(r.period)
			r.ready = true
			r.value = r.compute()
		}
		return r.value
	}

	// Wilder's smoothing: weight previous average by (period-1), new value by 1
	r.avgGain = (r.avgGain*float64(r.period-1) + gain) / float64(r.period)
	r.avgLoss = (r.avgLoss*float64(r.period-1) + loss) / float64(r.period)
	r.value = r.compute()
	return r.value
}

func (r *RSI) compute() float64 {
	if r.avgLoss == 0 {
		return 100
	}
	return 100 - 100/(1+r.avgGain/r.avgLoss)
}

func (r *RSI) Value() float64 { return r.value }
func (r *RSI) IsReady() bool  { return r.ready }
