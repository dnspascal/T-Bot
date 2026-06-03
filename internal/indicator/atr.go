package indicator

import "math"

type ATR struct {
	period      int
	value       float64
	prevClose   float64
	sum         float64
	count       int
	initialized bool
}

func NewATR(period int) *ATR {
	return &ATR{period: period}
}

// Add feeds one OHLC candle and returns the current ATR.
// Returns 0 until enough candles have been seen to seed the initial SMA.
func (a *ATR) Add(high, low, close float64) float64 {
	a.count++
	if a.count == 1 {
		a.prevClose = close
		return 0
	}

	tr := math.Max(math.Max(high-low, math.Abs(high-a.prevClose)), math.Abs(low-a.prevClose))
	a.prevClose = close

	if !a.initialized {
		a.sum += tr
		if a.count == a.period+1 {
			a.value = a.sum / float64(a.period)
			a.initialized = true
		}
		return a.value
	}

	a.value = (a.value*float64(a.period-1) + tr) / float64(a.period)
	return a.value
}

func (a *ATR) Value() float64  { return a.value }
func (a *ATR) IsReady() bool   { return a.initialized }

