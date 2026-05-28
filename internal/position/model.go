package position

import "time"

type Position struct {
	ID                 string
	OurOrderID         *string
	Provider           string
	ProviderPositionID string
	ProviderAcctID     string
	SymbolID           string
	Side               string // BUY | SELL
	Volume             int64
	OpenPrice          *float64
	CurrentSL          *float64
	CurrentTP          *float64
	Swap               float64
	Commission         float64
	UsedMargin         *float64
	Status             string // created | open | closed | error
	TrailingStopLoss   bool
	GuaranteedStopLoss bool
	Label              *string
	Comment            *string
	OpenTimestamp      *time.Time
	CloseTimestamp     *time.Time
	RawPayload         []byte
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
