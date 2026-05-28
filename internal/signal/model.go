package signal

import "time"

type Signal struct {
	ID           string
	SymbolID     string
	Signal       string // BUY | SELL | HOLD
	FastEMA      float64
	SlowEMA      float64
	RSI          float64
	Confluence   int    // 0=hold 1=weak 2=strong
	PriceMid     float64
	ProcessingMs int64
	CreatedAt    time.Time
}
