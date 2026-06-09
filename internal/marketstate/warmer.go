package marketstate

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/provider"
)

// Warmer pre-seeds the ProcessorManager with historical candles before live streaming starts.
// Must complete before StartStreaming() is called — the caller is responsible for that ordering.
type Warmer struct {
	prov            provider.Provider
	processorMgr    *ProcessorManager
	historicalCount int
}

func NewWarmer(prov provider.Provider, processorMgr *ProcessorManager, historicalCount int) *Warmer {
	return &Warmer{
		prov:            prov,
		processorMgr:    processorMgr,
		historicalCount: historicalCount,
	}
}

func (w *Warmer) WarmupAllTimeframes(ctx context.Context, symbol string) error {
	for i, period := range config.TradingPeriods {
		if i > 0 {
			time.Sleep(2 * time.Second)
		}
		if err := w.warmupTimeframe(ctx, symbol, period); err != nil {
			return fmt.Errorf("warmup %s: %w", period, err)
		}
	}
	slog.Info("all timeframes warmed up", "symbol", symbol, "timeframes", len(config.TradingPeriods))
	return nil
}

func (w *Warmer) warmupTimeframe(ctx context.Context, symbol, period string) error {
	candles, err := w.prov.FetchHistoricalCandles(ctx, symbol, period, w.historicalCount)
	if err != nil {
		return fmt.Errorf("fetch historical: %w", err)
	}
	if len(candles) == 0 {
		return fmt.Errorf("no historical candles returned")
	}

	for _, c := range candles {
		w.processorMgr.WarmCandle(period, c.OpenTime, c.Open, c.High, c.Low, c.Close, c.Volume)
	}

	if err := w.processorMgr.CommitWarmup(ctx, period); err != nil {
		slog.Warn("failed to commit warm-up state", "period", period, "err", err)
	}

	return nil
}
