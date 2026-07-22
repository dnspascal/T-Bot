package srbounce

import (
	"math"
	"time"

	"github.com/denismgaya/t-bot/internal/indicator"
	"github.com/denismgaya/t-bot/internal/ml"
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

type SRBounce struct {
	predictor *ml.Predictor
	threshold float32
	symbolID  float32 
	prevRSI   float64
}

func New(predictor *ml.Predictor, threshold float32, symbolID float32) *SRBounce {
	return &SRBounce{predictor: predictor, threshold: threshold, symbolID: symbolID}
}

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

	if s.predictor != nil {
		rsiVel := float32(m5.RSI - s.prevRSI)
		var rsiM15, rsiH1 float32
		if m15state, ok := states["M15"]; ok {
			rsiM15 = float32(m15state.RSI)
		}
		if h1state, ok := states["H1"]; ok {
			rsiH1 = float32(h1state.RSI)
		}
		isSell := float32(0)
		if direction == "SELL" {
			isSell = 1
		}
		aboveEMA50 := float32(0)
		if m5.EMA50 > 0 && currentPrice > m5.EMA50 {
			aboveEMA50 = 1
		}
		aboveEMA200 := float32(0)
		if m5.EMA200 > 0 && currentPrice > m5.EMA200 {
			aboveEMA200 = 1
		}
		prob, err := s.predictor.Predict(ml.Features{
			RSI:         float32(m5.RSI),
			RSIVel:      rsiVel,
			RSIM15:      rsiM15,
			RSIH1:       rsiH1,
			ATR:         float32(m5.ATR),
			AboveEMA50:  aboveEMA50,
			AboveEMA200: aboveEMA200,
			Hour:        float32(barHour(m5.BarTime)),
			Symbol:      s.symbolID,
			IsSell:      isSell,
		})
		if err == nil && prob < s.threshold {
			s.prevRSI = m5.RSI
			return hold("ml filter rejected")
		}
	}
	s.prevRSI = m5.RSI

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

	rsiDev := 50.0 - m5.RSI
	if direction == "SELL" {
		rsiDev = m5.RSI - 50.0
	}
	if rsiDev > 0 {
		score += math.Min(rsiDev/25.0, 1.0) * 50
	}

	rr := tpPips / slPips
	score += math.Min((rr-minRR)/minRR, 1.0) * 30

	if m5.Volume > 0 && m5.VolumeMA > 0 && m5.Volume > m5.VolumeMA {
		score += 20
	}

	return math.Min(score/100.0, 1.0)
}

func barHour(barTimeMs int64) int {
	return time.Unix(barTimeMs/1000, 0).UTC().Hour()
}
