package marketstate

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/denismgaya/t-bot/internal/api"
	"github.com/denismgaya/t-bot/internal/indicator"
)

type Warmer struct {
	client       *api.Client
	repo         Repository
	calculator   *indicator.Calculator
	provider     string
	historicalCount int 
}

func NewWarmer(client *api.Client, repo Repository, provider string, historicalCount int) *Warmer {
	return &Warmer{
		client:          client,
		repo:            repo,
		calculator:      indicator.NewCalculator(),
		provider:        provider,
		historicalCount: historicalCount,
	}
}

func (w *Warmer) WarmupAllTimeframes(ctx context.Context, symbolID string) error {
	periods := []struct {
		code uint32
		name string
	}{
		{api.PeriodM5, "M5"},
		{api.PeriodM15, "M15"},
		{api.PeriodM30, "M30"},
		{api.PeriodH1, "H1"},
		{api.PeriodH4, "H4"},
		{api.PeriodD1, "D1"},
	}

	for _, p := range periods {
		startTime := slog.Int64("start", 0)
		if err := w.warmupTimeframe(ctx, symbolID, p.code, p.name); err != nil {
			slog.Error("warmup failed", "period", p.name, "err", err)
			return fmt.Errorf("warmup %s: %w", p.name, err)
		}
		slog.Info("warmup complete", "period", p.name, startTime)
	}

	slog.Info("all timeframes warmed up", "count", len(periods))
	return nil
}

func (w *Warmer) warmupTimeframe(ctx context.Context, symbolID string, period uint32, periodName string) error {
	candles, err := w.client.FetchHistoricalTrendbars(period, w.historicalCount)
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
			w.provider,
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
