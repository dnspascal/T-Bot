package pnl

import "time"

type DailyPnL struct {
	ID                string
	Date              time.Time
	SymbolID          string
	RealizedPnL       float64
	GrossProfit       float64
	TotalCommission   float64
	TotalSwap         float64
	TradeCount        int
	WinCount          int
	LossCount         int
	AvgRoundTripMs    int64
	AvgSlippagePoints int64
	UpdatedAt         time.Time
}
