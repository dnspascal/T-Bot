package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/denismgaya/t-bot/internal/indicator"
)

// peakDrawbackThreshold: if price gives back this % of the peak gain, signal 1 fires.
const peakDrawbackThreshold = 40.0

// signalsToClose: how many reversal signals must agree before we close the position.
const signalsToClose = 3

// signalsToReduce: how many signals trigger a soft close (tier 2+ positions only).
const signalsToReduce = 2

// watchPositions is called on every M1 candle close.
// It updates high-water marks and checks each open position for reversal signals.
func (b *Bot) watchPositions(ctx context.Context, ms indicator.MarketState) {
	for _, pos := range b.registry.All() {
		// Keep MaxFavorable and MaxAdverse current.
		b.registry.UpdatePeaks(pos.ProviderPositionID, ms.Close)

		// Re-read after update so we work with fresh peaks.
		pos, ok := b.registry.Get(pos.ProviderPositionID)
		if !ok {
			continue
		}

		n, signals := countReversalSignals(ms, pos)
		if n == 0 {
			continue
		}

		slog.Info("reversal signals detected",
			"posID", pos.ProviderPositionID,
			"side", pos.Side,
			"count", n,
			"signals", strings.Join(signals, ", "),
			"maxFavorable", fmt.Sprintf("%.5f", pos.MaxFavorable),
			"currentPrice", fmt.Sprintf("%.5f", ms.Close),
		)

		switch {
		case n >= signalsToClose:
			slog.Info("3+ signals confirmed — closing position",
				"posID", pos.ProviderPositionID, "n", n,
			)
			b.closeTrackedPosition(ctx, pos)

		case n >= signalsToReduce && pos.Tier >= TierStronger:
			slog.Info("2 signals — reducing high-tier position",
				"posID", pos.ProviderPositionID, "tier", pos.Tier,
			)
			b.closeTrackedPosition(ctx, pos)
		}
	}
}

// countReversalSignals returns how many of the 5 reversal signals are firing,
// plus their names for logging.
func countReversalSignals(ms indicator.MarketState, pos trackedPosition) (int, []string) {
	var signals []string

	// Signal 1 — peak drawback: gave back ≥40% of peak gain from open
	if pct := peakDrawbackPct(pos, ms.Close); pct >= peakDrawbackThreshold {
		signals = append(signals, fmt.Sprintf("peak_drawback=%.0f%%", pct))
	}

	// Signal 2 — regime turned against position
	if (pos.Side == "BUY" && (ms.Regime == "trending_down" || ms.Regime == "ranging")) ||
		(pos.Side == "SELL" && (ms.Regime == "trending_up" || ms.Regime == "ranging")) {
		signals = append(signals, "regime_against")
	}

	// Signal 3 — RSI crossed midline against position
	if (pos.Side == "BUY" && ms.RSI < rsiMidline) ||
		(pos.Side == "SELL" && ms.RSI > rsiMidline) {
		signals = append(signals, "rsi_against")
	}

	// Signal 4 — EMA fast crossed slow against position
	if (pos.Side == "BUY" && ms.EMAFast < ms.EMASlow) ||
		(pos.Side == "SELL" && ms.EMAFast > ms.EMASlow) {
		signals = append(signals, "ema_cross_against")
	}

	// Signal 5 — momentum direction against position
	if (pos.Side == "BUY" && ms.MomentumDirection == "falling") ||
		(pos.Side == "SELL" && ms.MomentumDirection == "rising") {
		signals = append(signals, "momentum_against")
	}

	return len(signals), signals
}

// peakDrawbackPct returns what percentage of the peak gain has been given back.
// Returns 0 if the position was never in profit.
func peakDrawbackPct(pos trackedPosition, currentPrice float64) float64 {
	if pos.OpenPrice == 0 {
		return 0
	}

	var peakGain, currentGain float64
	if pos.Side == "BUY" {
		peakGain = pos.MaxFavorable - pos.OpenPrice
		currentGain = currentPrice - pos.OpenPrice
	} else {
		peakGain = pos.OpenPrice - pos.MaxFavorable
		currentGain = pos.OpenPrice - currentPrice
	}

	if peakGain <= 0 {
		return 0 // never went in profit — other signals handle this case
	}

	gaveBack := peakGain - currentGain
	if gaveBack <= 0 {
		return 0 // still at or above peak
	}

	return (gaveBack / peakGain) * 100
}

func (b *Bot) closeTrackedPosition(ctx context.Context, pos trackedPosition) {
	if _, err := b.provider.ClosePosition(ctx, pos.ProviderPositionID, pos.Volume); err != nil {
		slog.Error("watcher: ClosePosition failed",
			"posID", pos.ProviderPositionID, "err", err,
		)
		return
	}
	b.registry.Remove(pos.ProviderPositionID)
}
