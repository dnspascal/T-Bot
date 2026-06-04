package bot

import (
	"time"

	"github.com/denismgaya/t-bot/internal/indicator"
)

const (
	TierNormal     = 0 // 1× base risk
	TierStrong     = 1 // 2× base risk
	TierStronger   = 2 // 3× base risk
	TierVeryStrong = 3 // 4× base risk

	rsiMidline      = 50.0
	srProximityMult = 1.5 // within 1.5×ATR of S/R level counts as "near"
	slATRMult       = 1.5
	tpATRMult       = 2.5
	rangeBuffer     = 0.2 // buffer beyond S/R for ranging entries
	minRR           = 1.5 // minimum TP:SL ratio
)

// confluenceTimeframes are the higher timeframes checked for regime agreement.
var confluenceTimeframes = []string{"M15", "M30", "H1", "H4"}

// EntryResult is the output of evaluateEntry.
type EntryResult struct {
	Signal     string  // "BUY" | "SELL" | "HOLD"
	Confluence int
	Tier       int
	SLPrice    float64 // absolute price level, used by watcher
	TPPrice    float64 // absolute price level, used by watcher
	SLPips     float64 // for provider.PlaceMarketOrder (pipSize-relative)
	TPPips     float64 // for provider.PlaceMarketOrder
	ATR        float64 // M5 ATR at signal time
	Reason     string  // populated when Signal == "HOLD"
}

// inActiveSession returns true during EU and US sessions (07:00–21:00 UTC).
// BTC/USDT has deeper liquidity and cleaner trends in these windows.
func inActiveSession() bool {
	h := time.Now().UTC().Hour()
	return h >= 7 && h < 21
}

// evaluateEntry performs multi-timeframe confluence analysis.
// states should contain at minimum "M5"; higher timeframes improve accuracy.
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

	// M5 trigger: regime must be trending + RSI confirms direction.
	// CalculateRegime already enforces ADX >= 25 for trending_up/down.
	var direction string
	switch {
	case m5.Regime == "trending_up" && m5.RSI > rsiMidline:
		direction = "BUY"
	case m5.Regime == "trending_down" && m5.RSI < rsiMidline:
		direction = "SELL"
	default:
		return hold("no M5 trend trigger")
	}

	confluence := 1 // M5 trigger = baseline

	// Higher timeframe regime agreement
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

	// Volume conviction: current candle volume above 20-candle average
	if m5.Volume > 0 && m5.VolumeMA > 0 && m5.Volume > m5.VolumeMA {
		confluence++
	}

	// S/R proximity bonus: price is near a key level in the trade direction
	if direction == "BUY" && m5.SupportLevel > 0 {
		if (currentPrice - m5.SupportLevel) <= srProximityMult*m5.ATR {
			confluence++
		}
	}
	if direction == "SELL" && m5.ResistanceLevel > 0 {
		if (m5.ResistanceLevel - currentPrice) <= srProximityMult*m5.ATR {
			confluence++
		}
	}

	if confluence < 2 {
		return EntryResult{Signal: "HOLD", Confluence: confluence, Reason: "insufficient confluence"}
	}

	slPrice, tpPrice := computeSLTP(m5, direction, currentPrice)
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

// computeSLTP returns absolute price levels for SL and TP.
// Ranging markets use S/R levels; trending/breakout markets use ATR multiples.
func computeSLTP(m5 indicator.MarketState, direction string, price float64) (slPrice, tpPrice float64) {
	atr := m5.ATR
	if atr <= 0 {
		return 0, 0
	}

	// Ranging: anchor SL/TP to support and resistance
	if m5.Regime == "ranging" && m5.SupportLevel > 0 && m5.ResistanceLevel > 0 {
		if direction == "BUY" {
			sl := m5.SupportLevel - rangeBuffer*atr
			tp := m5.ResistanceLevel - rangeBuffer*atr
			if sl < price && tp > price {
				return sl, tp
			}
		} else {
			sl := m5.ResistanceLevel + rangeBuffer*atr
			tp := m5.SupportLevel + rangeBuffer*atr
			if sl > price && tp < price {
				return sl, tp
			}
		}
		// S/R levels are invalid for this direction — fall through to ATR
	}

	// Trending / breakout: ATR-based
	if direction == "BUY" {
		return price - slATRMult*atr, price + tpATRMult*atr
	}
	return price + slATRMult*atr, price - tpATRMult*atr
}

// pricesToPips converts absolute price levels to pip distances.
// pipSize is the bot-level constant (0.0001 for forex).
// For crypto instruments where this gives huge numbers, providers that don't use
// pip-based SL/TP (e.g. Binance spot) will ignore these values.
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
