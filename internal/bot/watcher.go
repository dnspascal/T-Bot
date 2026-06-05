package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/denismgaya/t-bot/internal/indicator"
)

const peakDrawbackThreshold = 40.0

// signalsToClose: how many reversal signals must agree before we close the position.
const signalsToClose = 3

// signalsToReduce: how many signals trigger a soft close (tier 2+ positions only).
const signalsToReduce = 2

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

		slog.Info("reversal signals detected")

		reason := strings.Join(signals, ",")
		switch {
		case n >= signalsToClose:
			slog.Info("3+ signals confirmed — closing position",
				"posID", pos.ProviderPositionID, "n", n,
			)
			b.closeTrackedPosition(ctx, pos, reason)

		case n >= signalsToReduce && pos.Tier >= TierStronger:
			slog.Info("2 signals — reducing high-tier position",
				"posID", pos.ProviderPositionID, "tier", pos.Tier,
			)
			b.closeTrackedPosition(ctx, pos, reason)
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

func (b *Bot) logM1State(ctx context.Context, currentPrice float64) {
	positions := b.registry.All()
	count := len(positions)
	provider := b.provider.Name()
	var totalUnrealized float64

	for _, pos := range positions {
		oldFav := pos.MaxFavorable
		oldAdv := pos.MaxAdverse

		b.registry.UpdatePeaks(pos.ProviderPositionID, currentPrice)

		pos, ok := b.registry.Get(pos.ProviderPositionID)
		if !ok {
			continue
		}

		var unrealized float64
		if pos.Side == "BUY" {
			unrealized = b.unrealizedUSD(currentPrice-pos.OpenPrice, pos.Volume)
		} else {
			unrealized = b.unrealizedUSD(pos.OpenPrice-currentPrice, pos.Volume)
		}
		totalUnrealized += unrealized

		drawback := peakDrawbackPct(pos, currentPrice)
		slog.Info("M1 position state",
			"provider", provider,
			"positions", count,
			"posID", pos.ProviderPositionID,
			"side", pos.Side,
			"open", pos.OpenPrice,
			"price", currentPrice,
			"pnlUSD", fmt.Sprintf("%+.4f", unrealized),
			"peakFavBefore", oldFav,
			"peakFavAfter", pos.MaxFavorable,
			"peakAdvBefore", oldAdv,
			"peakAdvAfter", pos.MaxAdverse,
			"drawbackPct", fmt.Sprintf("%.1f%%", drawback),
		)

		if drawback >= peakDrawbackThreshold {
			reason := fmt.Sprintf("peak_drawback=%.0f%%", drawback)
			slog.Info("peak drawback — closing position",
				"posID", pos.ProviderPositionID,
				"side", pos.Side,
				"drawback", fmt.Sprintf("%.0f%%", drawback),
			)
			b.closeTrackedPosition(ctx, pos, reason)
		}
	}

	if count > 1 {
		slog.Info("M1 total P&L",
			"provider", provider,
			"positions", count,
			"totalPnlUSD", fmt.Sprintf("%+.4f", totalUnrealized),
		)
	}
}

func (b *Bot) closeTrackedPosition(ctx context.Context, pos trackedPosition, reason string) {
	if _, err := b.provider.ClosePosition(ctx, pos.ProviderPositionID, pos.Volume); err != nil {
		slog.Error("watcher: ClosePosition failed",
			"posID", pos.ProviderPositionID, "err", err,
		)
		// INCORRECT_BOUNDARIES means the broker already closed the position (e.g. SL hit while bot was offline).
		// Remove from registry and mark closed in DB so reconcile doesn't reload it on next restart.
		if strings.Contains(err.Error(), "INCORRECT_BOUNDARIES") {
			slog.Warn("watcher: position already gone at broker — purging",
				"posID", pos.ProviderPositionID,
			)
			b.registry.Remove(pos.ProviderPositionID)
			if dbErr := b.positions.Close(ctx, b.provider.Name(), pos.ProviderPositionID, time.Now(), nil, nil); dbErr != nil {
				slog.Error("watcher: failed to mark orphaned position closed in DB", "posID", pos.ProviderPositionID, "err", dbErr)
			}
		}
		return
	}
	b.pendingCloseReasons[pos.ProviderPositionID] = reason
	b.registry.Remove(pos.ProviderPositionID)
}
