package signal

import "time"

// MarketStateSnapshot is the subset of indicator data stored per timeframe in checked_market_states.
type MarketStateSnapshot struct {
	MarketStateID     string  `json:"market_state_id,omitempty"`
	Regime            string  `json:"regime"`
	ADX               float64 `json:"adx"`
	RSI               float64 `json:"rsi"`
	EMAFast           float64 `json:"ema_fast"`
	EMASlow           float64 `json:"ema_slow"`
	ATR               float64 `json:"atr"`
	VolumeMA          int64   `json:"volume_ma"`
	MomentumDirection string  `json:"momentum_direction"`
	SupportLevel      float64 `json:"support_level"`
	ResistanceLevel   float64 `json:"resistance_level"`
}

type Signal struct {
	ID                   string
	SymbolID             string
	Provider             string
	Signal               string // BUY | SELL | HOLD
	Confluence           int
	ProcessingUS         int64
	CheckedMarketStates  map[string]MarketStateSnapshot // keyed by period: "M5", "H1", etc.
	BarTime              *time.Time
	CreatedAt            time.Time
}
