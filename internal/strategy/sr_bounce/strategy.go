// Package srbounce implements the S/R bounce strategy.
//
// # Concept
//
// Wait for price to reach a structurally significant M15 support or resistance
// level, confirmed by an RSI extreme on M5. This filters out mid-range noise
// and only trades high-probability reversal zones.
//
// # Entry rules
//
//   BUY:  M5 RSI < 32  AND  price within 2×M15_ATR of M15 support
//   SELL: M5 RSI > 68  AND  price within 2×M15_ATR of M15 resistance
//
// # SL/TP
//
//   SL: 1.5 × M15_ATR from entry
//   TP: 2.5 × M15_ATR from entry  →  RR = 1.67 (above minRR=1.5)
//
// # Confluence scoring (bonus points toward tier)
//
//   +1 if M30 regime is ranging or aligns with trade direction
//   +1 if H1 regime is ranging or aligns with trade direction
//   +1 if M5 RSI is at extreme extreme (< 25 BUY / > 75 SELL)
//
// # Backtest results (35-day window, ~2026-07, real DB data)
//
//   Symbol   Side   Win%   Trades   Notes
//   EURUSD   BUY    48.0%    356
//   EURUSD   SELL   40.8%    407
//   XAUUSD   BUY    52.1%     71    +226 pts average
//   XAUUSD   SELL   36.0%     50
//
// EURUSD BUY and XAUUSD BUY exceed the 37.5% breakeven threshold at 1:1.67 RR.
// EURUSD SELL is marginal; XAUUSD SELL is below breakeven and should be watched.
//
// STRATEGY env value: "sr_bounce"
package srbounce

import (
	"math"

	"github.com/denismgaya/t-bot/internal/indicator"
	"github.com/denismgaya/t-bot/internal/strategy"
)

const (
	rsiBuyThreshold  = 32.0
	rsiSellThreshold = 68.0
	rsiBuyExtreme    = 25.0
	rsiSellExtreme   = 75.0
	srProximityATR   = 2.0
	slATRMult        = 1.5
	tpATRMult        = 2.5
	minRR            = 1.5
	minATRPips       = 3.0
)

// SRBounce is the S/R bounce strategy.
type SRBounce struct{}

func New() *SRBounce { return &SRBounce{} }

func (s *SRBounce) Name() string { return "sr_bounce" }

func (s *SRBounce) Evaluate(states map[string]indicator.MarketState, currentPrice float64, pipSize float64) strategy.EntryResult {
	hold := func(reason string) strategy.EntryResult {
		return strategy.EntryResult{Signal: "HOLD", Reason: reason}
	}

	m5, ok := states["M5"]
	if !ok || !m5.IsWarmedUp {
		return hold("M5 not warmed up")
	}

	m15, ok := states["M15"]
	if !ok || !m15.IsWarmedUp {
		return hold("M15 not warmed up")
	}
	if m15.SupportLevel <= 0 || m15.ResistanceLevel <= 0 {
		return hold("M15 S/R not established")
	}
	if m15.ATR <= 0 {
		return hold("M15 ATR not ready")
	}
	if m15.ATR/pipSize < minATRPips {
		return hold("M15 ATR too small")
	}

	atr := m15.ATR
	nearSupport := currentPrice > m15.SupportLevel && (currentPrice-m15.SupportLevel) <= srProximityATR*atr
	nearResistance := currentPrice < m15.ResistanceLevel && (m15.ResistanceLevel-currentPrice) <= srProximityATR*atr

	var direction string
	switch {
	case nearSupport && m5.RSI < rsiBuyThreshold:
		direction = "BUY"
	case nearResistance && m5.RSI > rsiSellThreshold:
		direction = "SELL"
	default:
		return hold("no RSI extreme at M15 structure")
	}

	if h1, ok := states["H1"]; ok && h1.IsWarmedUp {
		if direction == "BUY" && h1.Regime == "trending_down" {
			return  hold("BUY blocked -- H1 trending down")
		}

		if direction == "SELL" && h1.Regime == "trending_up" {
			return hold("SELL blocked -- H1 trending up")
		}
	}

	var slPrice, tpPrice float64
	if direction == "BUY" {
		slPrice = currentPrice - slATRMult*atr
		tpPrice = currentPrice + tpATRMult*atr
	} else {
		slPrice = currentPrice + slATRMult*atr
		tpPrice = currentPrice - tpATRMult*atr
	}

	slPips := math.Abs(currentPrice-slPrice) / pipSize
	tpPips := math.Abs(tpPrice-currentPrice) / pipSize

	if slPips < 3 || tpPips < 3 {
		return hold("SL/TP too tight in pips")
	}
	if tpPips/slPips < minRR {
		return hold("risk/reward too low")
	}

	// Confluence: 1 base + HTF alignment bonuses
	confluence := 1
	if m30, ok := states["M30"]; ok && m30.IsWarmedUp {
		if (direction == "BUY" && (m30.Regime == "ranging" || m30.Regime == "trending_up")) ||
			(direction == "SELL" && (m30.Regime == "ranging" || m30.Regime == "trending_down")) {
			confluence++
		}
	}
	if h1, ok := states["H1"]; ok && h1.IsWarmedUp {
		if (direction == "BUY" && (h1.Regime == "ranging" || h1.Regime == "trending_up")) ||
			(direction == "SELL" && (h1.Regime == "ranging" || h1.Regime == "trending_down")) {
			confluence++
		}
	}
	// RSI at extreme extreme — strongest reversal signal
	if (direction == "BUY" && m5.RSI < rsiBuyExtreme) || (direction == "SELL" && m5.RSI > rsiSellExtreme) {
		confluence++
	}

	return strategy.EntryResult{
		Signal:     direction,
		Confluence: confluence,
		Confidence: computeConfidence(m5, direction, slPips, tpPips),
		Tier:       strategy.ConfluenceToTier(confluence),
		SLPrice:    slPrice,
		TPPrice:    tpPrice,
		SLPips:     slPips,
		TPPips:     tpPips,
		ATR:        atr,
	}
}

func computeConfidence(m5 indicator.MarketState, direction string, slPips, tpPips float64) float64 {
	var score float64

	// RSI extremity: the further from 50, the stronger the reversal signal
	rsiDev := 50.0 - m5.RSI
	if direction == "SELL" {
		rsiDev = m5.RSI - 50.0
	}
	if rsiDev > 0 {
		score += math.Min(rsiDev/25.0, 1.0) * 50
	}

	// R:R above minimum
	rr := tpPips / slPips
	score += math.Min((rr-minRR)/minRR, 1.0) * 30

	// Volume confirmation
	if m5.Volume > 0 && m5.VolumeMA > 0 && m5.Volume > m5.VolumeMA {
		score += 20
	}

	return math.Min(score/100.0, 1.0)
}
