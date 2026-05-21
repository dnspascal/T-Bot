package candle

import "time"

type Candle struct {
	ID         string
	Symbol     string
	SymbolID   int64
	Period     string // M1 M5 M15 M30 H1 H4 D1 W1 MN1
	Open       float64
	High       float64
	Low        float64
	Close      float64
	TickVolume int64
	BarTime    time.Time
	ReceivedAt time.Time
}
