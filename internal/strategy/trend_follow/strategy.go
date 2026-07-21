package trendfollow

import (
	"math"

	"github.com/denismgaya/t-bot/internal/indicator"
	"github.com/denismgaya/t-bot/internal/strategy"
)

type TrendFollow struct{}

const (
	slATRMult = 1.5
	tpATRMult = 2.5
)

func New() *TrendFollow { return &TrendFollow{} }

func (s *TrendFollow) Name() string { return "trend_follow" }

func (s *TrendFollow) Evaluate(states map[string]indicator.MarketState, currentPrice, pipSize float64) strategy.EntryResult {

	hold := func(rsn string) strategy.EntryResult {
		return strategy.EntryResult{Signal: strategy.SignalHold, Reason: rsn}
	}

	h1, ok := states["H1"]
	if !ok || !h1.IsWarmedUp {
		return hold("H1 not warmed up")
	}

	if h1.Regime == indicator.Ranging {
		return hold("H1 regime is ranging -- sr_bounce handles this")
	}

	var dir string

	switch h1.Regime {
	case indicator.TrendingUp:
		dir = strategy.SignalBuy
	case indicator.TrendingDown:
		dir = strategy.SignalSell
	default:
		return hold("H1 regime unclear")
	}

	m5, ok := states["M5"]
	if !ok || !m5.IsWarmedUp {
		return hold("M5 not warmed up")
	}

	if dir == strategy.SignalBuy && m5.EMAFast > m5.EMASlow {
		return hold("no pullback - price still above fast EMA")
	}

	if dir == strategy.SignalSell && m5.EMAFast < m5.EMASlow {
		return hold("no pullback - price still below fast EMA")
	}

	m15, ok := states["M15"]
	if !ok || !m15.IsWarmedUp || m15.ATR <= 0 {
		return hold("M15 ATR not ready")
	}

	atr := m15.ATR
	var slPrice, tpPrice float64
	if dir == strategy.SignalBuy {
		slPrice = currentPrice - slATRMult*atr
		tpPrice = currentPrice + tpATRMult*atr
	} else {
		slPrice = currentPrice + slATRMult*atr
		tpPrice = currentPrice - tpATRMult*atr
	}

	slPips := math.Abs(currentPrice-slPrice) / pipSize
	tpPips := math.Abs(tpPrice-currentPrice) / pipSize

	return strategy.EntryResult{
		Signal:     dir,
		Confluence: 1,
		Tier:       strategy.TierNormal,
		SLPrice:    slPrice,
		TPPrice:    tpPrice,
		SLPips:     slPips,
		TPPips:     tpPips,
		ATR:        atr,
	}

}
