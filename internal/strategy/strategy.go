package strategy

import "github.com/denismgaya/t-bot/internal/indicator"

const (
	TierNormal     = 0
	TierStrong     = 1
	TierStronger   = 2
	TierVeryStrong = 3
)

const (
	SignalBuy  = "BUY"
	SignalSell = "SELL"
	SignalHold = "HOLD"
)

type EntryResult struct {
	Signal       string
	Confluence   int
	Confidence   float64 
	Tier         int
	SLPrice      float64
	TPPrice      float64
	SLPips       float64
	TPPips       float64
	ATR          float64
	StrategyName string
	Reason       string
}

type Strategy interface {
	Name() string

	Evaluate(states map[string]indicator.MarketState, currentPrice, pipSize float64) EntryResult
}

func ConfluenceToTier(c int) int {
	switch {
	case c >= 6:
		return TierVeryStrong
	case c >= 5:
		return TierStronger
	case c >= 4:
		return TierStrong
	default:
		return TierNormal
	}
}
