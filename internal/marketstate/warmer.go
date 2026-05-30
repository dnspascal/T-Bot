package marketstate

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/denismgaya/t-bot/internal/indicator"
	"github.com/denismgaya/t-bot/internal/provider"
)

type Warmer struct {
	prov            provider.Provider
	repo            Repository
	calculator      *indicator.Calculator
	providerName    string
	historicalCount int
}

func NewWarmer(prov provider.Provider, repo Repository, providerName string, historicalCount int) *Warmer {
	return &Warmer{
		prov:            prov,
		repo:            repo,
		calculator:      indicator.NewCalculator(),
		providerName:    providerName,
		historicalCount: historicalCount,
	}
}

func (w *Warmer) WarmupAllTimeframes(ctx context.Context, symbolID string) error {
	periods := []string{"M5", "M15", "M30", "H1", "H4", "D1"}

	for _, periodName := range periods {
		if err := w.warmupTimeframe(ctx, symbolID, periodName); err != nil {
			slog.Error("warmup failed", "period", periodName, "err", err)
			return fmt.Errorf("warmup %s: %w", periodName, err)
		}
		slog.Info("warmup complete", "period", periodName)
	}

	slog.Info("all timeframes warmed up", "count", len(periods))
	return nil
}

func (w *Warmer) warmupTimeframe(ctx context.Context, symbolID string, periodName string) error {
	candles, err := w.prov.FetchHistoricalCandles(ctx, "", periodName, w.historicalCount)
	if err != nil {
		return fmt.Errorf("fetch historical %s: %w", periodName, err)
	}

	if len(candles) == 0 {
		return fmt.Errorf("no historical candles returned for %s", periodName)
	}

	slog.Info("loaded historical candles", "period", periodName, "count", len(candles))

	var closes []float64
	var ohlcData []indicator.OHLC

	for _, c := range candles {
		closes = append(closes, c.Close)
		ohlcData = append(ohlcData, indicator.OHLC{
			High:  c.High,
			Low:   c.Low,
			Close: c.Close,
		})
	}

	for i := 0; i < len(candles); i++ {
		candle := candles[i]

		historicalCloses := closes[:i+1]
		historicalOHLC := ohlcData[:i+1]

		marketState := w.calculator.CalculateFromHistory(
			symbolID,
			w.providerName,
			periodName,
			candle.OpenTime,
			candle.Open,
			candle.High,
			candle.Low,
			candle.Close,
			candle.Volume,
			historicalCloses,
			historicalOHLC,
		)

		if err := w.repo.Insert(ctx, marketState); err != nil {
			return fmt.Errorf("insert market state: %w", err)
		}
	}

	slog.Info("stored market states", "period", periodName, "count", len(candles))
	return nil
}
