package main

import (
	"context"
	"log"
	"log/slog"
	"time"

	"github.com/denismgaya/t-bot/internal/bot"
	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/marketstate"
	"github.com/denismgaya/t-bot/internal/provider"
	"github.com/denismgaya/t-bot/internal/risk"
)

// BotInitResult holds the initialized bot and its dependencies
type BotInitResult struct {
	Bot            *bot.Bot
	RiskManager    *risk.Manager
	ProcessorMgr   *marketstate.ProcessorManager
	Balance        float64
	HasOpenPosition bool
}

// initializeBot sets up the trading bot with all dependencies
func initializeBot(ctx context.Context, cfg *config.Config, svc *Services, prov provider.Provider, symbol string, symbolUUID string, authResult *provider.AuthResult) *BotInitResult {
	balance := authResult.Balance
	hasOpenPosition := authResult.HasOpenPosition
	// Setup risk manager
	todayLoss, err := svc.Repos.PnLs.Today(ctx, symbolUUID)
	if err != nil {
		log.Fatal("load daily pnl:", err)
	}
	riskMgr := risk.New(cfg.RiskPercent, cfg.MaxDailyLoss)
	if todayLoss < 0 {
		riskMgr.RestoreLoss(-todayLoss)
	}
	slog.Info("daily pnl restored", "todayLoss", todayLoss, "provider", prov.Name(), "symbol", symbol)

	// Warmup market states from provider
	warmerStart := time.Now()
	warmer := marketstate.NewWarmer(prov, svc.Repos.MarketState, prov.Name(), 50)
	if err := warmer.WarmupAllTimeframes(ctx, symbol); err != nil {
		slog.Warn("warmup failed", "err", err)
	}
	slog.Info("warmup complete", "elapsedMs", elapsed(warmerStart))

	// Create processor manager for live market state calculation
	processorMgr := marketstate.NewProcessorManager(symbolUUID, prov.Name(), svc.Repos.MarketState)

	// Create a processor for each trading timeframe
	tradingPeriods := []string{"M5", "M15", "M30", "H1", "H4", "D1"}

	for _, period := range tradingPeriods {
		buf := marketstate.NewMemoryCandleBuffer(21)
		proc := marketstate.NewProcessor(symbolUUID, prov.Name(), period, buf, svc.Repos.MarketState)
		processorMgr.AddProcessor(period, proc)
	}
	slog.Info("market state processors initialized", "timeframes", len(tradingPeriods), "symbol", symbol)

	// Create bot instance with provider account ID
	tradingBot := bot.New(cfg, prov, symbol, symbolUUID, authResult.AccountID, svc.DB.Pool, riskMgr, balance, hasOpenPosition, svc.Lookup, svc.Repos.Ticks, svc.Repos.Candles, svc.Repos.Signals, svc.Repos.Orders, svc.Repos.Fills, svc.Repos.Positions, svc.Repos.PnLs, svc.Repos.Events, processorMgr)

	return &BotInitResult{
		Bot:             tradingBot,
		RiskManager:     riskMgr,
		ProcessorMgr:    processorMgr,
		Balance:         balance,
		HasOpenPosition: hasOpenPosition,
	}
}
