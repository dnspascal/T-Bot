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

	rsiMidline      = 50.0
	rsiOversold     = 40.0 // ranging BUY: RSI below this = momentum exhausted at support
	rsiOverbought   = 60.0 // ranging SELL: RSI above this = momentum exhausted at resistance
	srProximityMult = 1.5  // within 1.5×ATR of S/R counts as "near"
	srStabilityMult = 1.5  // max allowed spread between TF S/R levels, in ATR units
	slATRMult       = 1.5
	tpATRMult       = 2.5
	slRangeBuffer   = 0.25 // SL: 25% of ATR outside S/R — absorbs wicks
	tpRangeBuffer   = 0.15 // TP: 15% of ATR inside S/R — fills before reversal
	minRR           = 1.5 // minimum TP:SL ratio
)

var confluenceTimeframes = []string{"M15", "M30", "H1", "H4"}

var rangeRequiredTFs = []string{"M15", "M30"}

var rangeBonusTFs = []string{"H1", "H4"}

type EntryResult struct {
	Signal     string  
	Confluence int
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

func inActiveSession() bool {
	h := time.Now().UTC().Hour()
	return h >= 7 && h < 21
}

func evaluateEntry(states map[string]indicator.MarketState, currentPrice float64) EntryResult {
	hold := func(reason string) EntryResult {
		return EntryResult{Signal: "HOLD", Reason: reason}
	}

	m5, ok := states["M5"]
	if !ok || !m5.IsWarmedUp {
		return hold("M5 not warmed up")
	}
	if !inActiveSession() {
		return hold("outside active session")
	}

	var direction string
	var isRanging bool
	var rangeConf rangeConfirmation

	switch {
	case m5.Regime == "trending_up" && m5.RSI > rsiMidline:
		direction = "BUY"
	case m5.Regime == "trending_down" && m5.RSI < rsiMidline:
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
		slPrice, tpPrice = computeRangeSLTP(rangeConf, direction, currentPrice, m5.ATR)
	} else {
		slPrice, tpPrice = computeSLTP(m5, direction, currentPrice)
	}

	if slPrice <= 0 || tpPrice <= 0 {
		return hold("cannot compute valid SL/TP")
	}

	slPips, tpPips := pricesToPips(direction, currentPrice, slPrice, tpPrice)
	if slPips < 5 || tpPips < 5 {
		return hold("SL/TP too tight in pips")
	}
	if tpPips/slPips < minRR {
		return hold("risk/reward too low")
	}

	return EntryResult{
		Signal:     direction,
		Confluence: confluence,
		Tier:       confluenceToTier(confluence),
		SLPrice:    slPrice,
		TPPrice:    tpPrice,
		SLPips:     slPips,
		TPPips:     tpPips,
		ATR:        m5.ATR,
	}
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
