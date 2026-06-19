package bot

import (
	"slices"
	"time"

	"github.com/denismgaya/t-bot/internal/indicator"
)

const (
	TierNormal     = 0 
	TierStrong     = 1 
	TierStronger   = 2 
	TierVeryStrong = 3 

	rsiMidline    = 50.0
	rsiOversold   = 44.0
	rsiOverbought = 56.0
	srProximityMult = 1.5
	srStabilityMult = 1.5
	slATRMult       = 1.5
	tpATRMult       = 2.5
	slRangeBuffer   = 0.25
	tpRangeBuffer   = 0.15
	minRR           = 1.5
	minATRPips      = 3.0 // M5 ATR below this means spread eats the SL — skip the trade
)

var confluenceTimeframes = []string{"M15", "M30", "H1", "H4"}

var rangeRequiredTFs = []string{"M15", "M30"}

var rangeBonusTFs = []string{"H1", "H4"}

type EntryResult struct {
	Signal     string
	Confluence int
	Confidence float64 
	Tier       int
	SLPrice    float64
	TPPrice    float64
	SLPips     float64
	TPPips     float64
	ATR        float64
	Reason     string
}

type rangeConfirmation struct {
	confirmed           bool
	consensusSupport    float64
	consensusResistance float64
	bonusTFs            int 
}

type fxSession int

const (
	sessionDead    fxSession = iota // 22:00–22:59 UTC: true gap between NY close and Sydney ramp
	sessionTokyo                    // 23:00–06:59 UTC: Sydney/Tokyo
	sessionLondon                   // 07:00–12:59 UTC: London only
	sessionLondonNY                 // 13:00–15:59 UTC: peak overlap
	sessionNewYork                  // 16:00–21:59 UTC: NY only (full window)
)

func classifySession(h int) fxSession {
	switch {
	case h == 22:
		return sessionDead
	case h >= 13 && h < 16:
		return sessionLondonNY
	case h >= 7 && h < 13:
		return sessionLondon
	case h >= 16 && h < 22:
		return sessionNewYork // 16:00–21:59 UTC
	default: // 23:00–06:59
		return sessionTokyo
	}
}

func inActiveSession(londonNYOnly bool) bool {
	s := classifySession(time.Now().UTC().Hour())
	if londonNYOnly {
		return s == sessionLondon || s == sessionLondonNY || s == sessionNewYork
	}
	return s != sessionDead
}

func evaluateEntry(states map[string]indicator.MarketState, currentPrice float64, londonNYOnly bool) EntryResult {
	hold := func(reason string) EntryResult {
		return EntryResult{Signal: "HOLD", Reason: reason}
	}

	m5, ok := states["M5"]
	if !ok || !m5.IsWarmedUp {
		return hold("M5 not warmed up")
	}
	if !inActiveSession(londonNYOnly) {
		return hold("outside active session")
	}
	if isEODWindow() {
		return hold("EOD window — no new entries before dead session")
	}

	var direction string
	var isRanging bool
	var rangeConf rangeConfirmation
	var rangeATR = m5.ATR

	if m5.Regime != "ranging" {
		if htfConf, htfAnchor, ok := confirmHigherTFRange(states); ok {
			dir := rangingDirection(htfAnchor, htfConf, currentPrice)
			if dir == "" {
				return hold("M15+M30 ranging: price not near S/R or RSI not confirming")
			}
			direction = dir
			rangeConf = htfConf
			isRanging = true
			rangeATR = htfAnchor.ATR
		}
	}

	if !isRanging {
		switch {
		case m5.Regime == "trending_up" && m5.RSI > rsiMidline && m5.RSI < rsiOverbought:
			direction = "BUY"
		case m5.Regime == "trending_down" && m5.RSI < rsiMidline && m5.RSI > rsiOversold:
			direction = "SELL"

		// Breakout: price broke the 20-bar range. Trade in the direction confirmed by EMA + RSI.
		case m5.Regime == "breakout" && m5.EMAFast > m5.EMASlow && m5.RSI > rsiMidline && m5.RSI < rsiOverbought:
			direction = "BUY"
		case m5.Regime == "breakout" && m5.EMAFast < m5.EMASlow && m5.RSI < rsiMidline && m5.RSI > rsiOversold:
			direction = "SELL"

		case m5.Regime == "ranging":
			rangeConf = confirmRange(m5, states)
			if !rangeConf.confirmed {
				return hold("ranging: not confirmed across timeframes or S/R unstable")
			}
			direction = rangingDirection(m5, rangeConf, currentPrice)
			if direction == "" {
				return hold("ranging: price not near confirmed S/R or RSI not confirming")
			}
			isRanging = true

		default:
			return hold("no M5 trend trigger")
		}
	}

	// ATR check is regime-aware: ranging trades use M15 ATR for SL/TP so check that;
	// trending/breakout use M5 ATR so check that instead.
	if isRanging {
		if m15, ok := states["M15"]; ok && m15.ATR/pipSize < minATRPips {
			return hold("M15 ATR too small for ranging SL/TP")
		}
	} else {
		if m5.ATR/pipSize < minATRPips {
			return hold("M5 ATR too small — spread would eat SL")
		}
	}

	confluence := 1

	if isRanging {
		confluence += rangeConf.bonusTFs
	} else {
		for _, tf := range confluenceTimeframes {
			s, ok := states[tf]
			if !ok || !s.IsWarmedUp {
				continue
			}
			if direction == "BUY" && s.Regime == "trending_up" {
				confluence++
			} else if direction == "SELL" && s.Regime == "trending_down" {
				confluence++
			}
		}
		if direction == "BUY" && m5.SupportLevel > 0 && (currentPrice-m5.SupportLevel) <= srProximityMult*m5.ATR {
			confluence++
		}
		if direction == "SELL" && m5.ResistanceLevel > 0 && (m5.ResistanceLevel-currentPrice) <= srProximityMult*m5.ATR {
			confluence++
		}
	}

	if m5.Volume > 0 && m5.VolumeMA > 0 && m5.Volume > m5.VolumeMA {
		confluence++
	}

	if confluence < 2 {
		return EntryResult{Signal: "HOLD", Confluence: confluence, Reason: "insufficient confluence"}
	}


	var slPrice, tpPrice float64
	if isRanging {
		slPrice, tpPrice = computeRangeSLTP(rangeConf, direction, currentPrice, rangeATR)
	} else {
		slPrice, tpPrice = computeSLTP(m5, direction, currentPrice)
	}

	if slPrice <= 0 || tpPrice <= 0 {
		return hold("cannot compute valid SL/TP")
	}

	slPips, tpPips := pricesToPips(direction, currentPrice, slPrice, tpPrice)
	if slPips < 3 || tpPips < 3 {
		return hold("SL/TP too tight in pips")
	}
	if tpPips/slPips < minRR {
		return hold("risk/reward too low")
	}

	return EntryResult{
		Signal:     direction,
		Confluence: confluence,
		Confidence: computeConfidence(m5, states, direction, slPips, tpPips),
		Tier:       confluenceToTier(confluence),
		SLPrice:    slPrice,
		TPPrice:    tpPrice,
		SLPips:     slPips,
		TPPips:     tpPips,
		ATR:        m5.ATR,
	}
}


func computeConfidence(m5 indicator.MarketState, states map[string]indicator.MarketState, direction string, slPips, tpPips float64) float64 {
	var score float64

	rsiDev := m5.RSI - rsiMidline
	if direction == "SELL" {
		rsiDev = rsiMidline - m5.RSI
	}
	if rsiDev < 0 {
		rsiDev = 0
	}
	score += min(rsiDev/10.0, 1.0) * 25 // max 25 points

	if m5.ADX > 0 {
		score += min((m5.ADX-20)/20.0, 1.0) * 25
	}

	if m5.ATR > 0 {
		spread := m5.EMAFast - m5.EMASlow
		if direction == "SELL" {
			spread = m5.EMASlow - m5.EMAFast
		}
		score += min(spread/(m5.ATR*2), 1.0) * 20
	}

	for _, tf := range confluenceTimeframes {
		s, ok := states[tf]
		if !ok || !s.IsWarmedUp {
			continue
		}
		if direction == "BUY" && s.Regime == "trending_up" {
			score += 5
		} else if direction == "SELL" && s.Regime == "trending_down" {
			score += 5
		}
	}

	rr := tpPips / slPips
	score += min((rr-minRR)/minRR, 1.0) * 10

	return min(score/100.0, 1.0)
}

func confirmRange(m5 indicator.MarketState, states map[string]indicator.MarketState) rangeConfirmation {
	if m5.SupportLevel <= 0 || m5.ResistanceLevel <= 0 || m5.ATR <= 0 {
		return rangeConfirmation{}
	}

	supports := []float64{m5.SupportLevel}
	resistances := []float64{m5.ResistanceLevel}

	for _, tf := range rangeRequiredTFs {
		s, ok := states[tf]
		if !ok || !s.IsWarmedUp || s.Regime != "ranging" {
			return rangeConfirmation{}
		}
		if s.SupportLevel <= 0 || s.ResistanceLevel <= 0 {
			return rangeConfirmation{}
		}
		supports = append(supports, s.SupportLevel)
		resistances = append(resistances, s.ResistanceLevel)
	}

	bonusTFs := 0
	for _, tf := range rangeBonusTFs {
		s, ok := states[tf]
		if !ok || !s.IsWarmedUp || s.Regime != "ranging" {
			continue
		}
		if s.SupportLevel <= 0 || s.ResistanceLevel <= 0 {
			continue
		}
		supports = append(supports, s.SupportLevel)
		resistances = append(resistances, s.ResistanceLevel)
		bonusTFs++
	}

	tolerance := srStabilityMult * m5.ATR
	if slices.Max(supports)-slices.Min(supports) > tolerance {
		return rangeConfirmation{}
	}
	if slices.Max(resistances)-slices.Min(resistances) > tolerance {
		return rangeConfirmation{}
	}

	consensusSupport := slices.Max(supports)
	consensusResistance := slices.Min(resistances)

	if consensusResistance <= consensusSupport {
		return rangeConfirmation{}
	}

	return rangeConfirmation{
		confirmed:           true,
		consensusSupport:    consensusSupport,
		consensusResistance: consensusResistance,
		bonusTFs:            bonusTFs,
	}
}


func confirmHigherTFRange(states map[string]indicator.MarketState) (rangeConfirmation, indicator.MarketState, bool) {
	m15, ok := states["M15"]
	if !ok || !m15.IsWarmedUp || m15.Regime != "ranging" || m15.SupportLevel <= 0 || m15.ResistanceLevel <= 0 || m15.ATR <= 0 {
		return rangeConfirmation{}, indicator.MarketState{}, false
	}
	m30, ok := states["M30"]
	if !ok || !m30.IsWarmedUp || m30.Regime != "ranging" || m30.SupportLevel <= 0 || m30.ResistanceLevel <= 0 {
		return rangeConfirmation{}, indicator.MarketState{}, false
	}

	supports := []float64{m15.SupportLevel, m30.SupportLevel}
	resistances := []float64{m15.ResistanceLevel, m30.ResistanceLevel}

	bonusTFs := 0
	for _, tf := range rangeBonusTFs {
		s, ok := states[tf]
		if !ok || !s.IsWarmedUp || s.Regime != "ranging" || s.SupportLevel <= 0 || s.ResistanceLevel <= 0 {
			continue
		}
		supports = append(supports, s.SupportLevel)
		resistances = append(resistances, s.ResistanceLevel)
		bonusTFs++
	}

	tolerance := srStabilityMult * m15.ATR
	if slices.Max(supports)-slices.Min(supports) > tolerance {
		return rangeConfirmation{}, indicator.MarketState{}, false
	}
	if slices.Max(resistances)-slices.Min(resistances) > tolerance {
		return rangeConfirmation{}, indicator.MarketState{}, false
	}

	consensusSupport := slices.Max(supports)
	consensusResistance := slices.Min(resistances)
	if consensusResistance <= consensusSupport {
		return rangeConfirmation{}, indicator.MarketState{}, false
	}

	return rangeConfirmation{
		confirmed:           true,
		consensusSupport:    consensusSupport,
		consensusResistance: consensusResistance,
		bonusTFs:            bonusTFs,
	}, m15, true
}

func rangingDirection(m5 indicator.MarketState, rc rangeConfirmation, price float64) string {
	nearSupport := price > rc.consensusSupport && (price-rc.consensusSupport) <= srProximityMult*m5.ATR
	nearResistance := price < rc.consensusResistance && (rc.consensusResistance-price) <= srProximityMult*m5.ATR

	if nearSupport && m5.RSI < rsiOversold {
		return "BUY"
	}
	if nearResistance && m5.RSI > rsiOverbought {
		return "SELL"
	}
	return ""
}

func computeRangeSLTP(rc rangeConfirmation, direction string, price, atr float64) (slPrice, tpPrice float64) {
	if direction == "BUY" {
		sl := rc.consensusSupport - slRangeBuffer*atr
		tp := rc.consensusResistance - tpRangeBuffer*atr
		if sl < price && tp > price {
			return sl, tp
		}
	} else {
		sl := rc.consensusResistance + slRangeBuffer*atr
		tp := rc.consensusSupport + tpRangeBuffer*atr
		if sl > price && tp < price {
			return sl, tp
		}
	}
	return 0, 0
}

func computeSLTP(m5 indicator.MarketState, direction string, price float64) (slPrice, tpPrice float64) {
	atr := m5.ATR
	if atr <= 0 {
		return 0, 0
	}
	if direction == "BUY" {
		return price - slATRMult*atr, price + tpATRMult*atr
	}
	return price + slATRMult*atr, price - tpATRMult*atr
}

func confluenceToTier(c int) int {
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

func pricesToPips(direction string, entry, slPrice, tpPrice float64) (slPips, tpPips float64) {
	if direction == "BUY" {
		slPips = (entry - slPrice) / pipSize
		tpPips = (tpPrice - entry) / pipSize
	} else {
		slPips = (slPrice - entry) / pipSize
		tpPips = (entry - tpPrice) / pipSize
	}
	return
}
