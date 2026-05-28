package fill

import "time"

type Fill struct {
	ID                 string
	OurOrderID         *string
	OurPositionID      *string
	Provider           string
	ProviderFillID     string
	ProviderOrderID    *string
	ProviderPositionID *string
	SymbolID           string
	Side               string
	Volume             *int64
	FilledVolume       *int64
	ExecutionPrice     *float64
	EventType          string
	FillStatus         *string
	Commission         *float64
	MarginRate         *float64
	BaseToUSDRate      *float64
	CloseEntryPrice    *float64
	GrossProfit        *float64
	CloseSwap          *float64
	CloseCommission    *float64
	BalanceAfter       *float64
	ClosedVolume       *int64
	PnLConversionFee   *float64
	TradeDurationMs    *int64
	NetProfit          *float64
	ProviderCreateTime *time.Time
	ProviderExecTime   *time.Time
	RawPayload         []byte
	ReceivedAt         time.Time
}
