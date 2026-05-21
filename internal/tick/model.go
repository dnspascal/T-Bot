package tick

import "time"

type Tick struct {
	ID                string
	Symbol            string
	SymbolID          int64
	Bid               float64
	Ask               float64
	Mid               float64
	Spread            float64
	SessionClose      *float64
	ProviderTimestamp *time.Time
	ReceivedAt        time.Time
	ProcessingMs      int64
}
