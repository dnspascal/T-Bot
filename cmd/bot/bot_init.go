package main

import (
	"context"
	"log"
	"log/slog"

	"github.com/denismgaya/t-bot/internal/bot"
	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/marketstate"
	"github.com/denismgaya/t-bot/internal/provider"
	"github.com/denismgaya/t-bot/internal/risk"
)

type BotInitResult struct {
	Bot            *bot.Bot
	RiskManager    *risk.Manager
	ProcessorMgr   *marketstate.ProcessorManager
	Balance        float64
	HasOpenPosition bool
}

func initializeBot(ctx context.Context, cfg *config.Config, svc *Services, prov provider.Provider, symbol string, symbolUUID string, authResult *provider.AuthResult) *BotInitResult {
	balance := authResult.Balance
	hasOpenPosition := authResult.HasOpenPosition

	todayLoss, err := svc.Repos.PnLs.Today(ctx, symbolUUID)
	if err != nil {
		log.Fatal("load daily pnl:", err)
	}
	riskMgr := risk.New(cfg.RiskPercent, cfg.MaxDailyLoss)
	if todayLoss < 0 {
		riskMgr.RestoreLoss(-todayLoss)
	}

	processorMgr := marketstate.NewProcessorManager(symbolUUID, prov.Name(), svc.Repos.MarketState)

	for _, period := range config.TradingPeriods {
		buf := marketstate.NewMemoryCandleBuffer(21)
		proc := marketstate.NewProcessor(symbolUUID, prov.Name(), period, buf, svc.Repos.MarketState)
		processorMgr.AddProcessor(period, proc)
	}
	slog.Info("market state processors initialized", "timeframes", len(config.TradingPeriods), "symbol", symbol)

	tradingBot := bot.New(cfg, prov, symbol, symbolUUID, authResult.AccountID, svc.DB.Pool, riskMgr, balance, hasOpenPosition, svc.Lookup, svc.Repos.Ticks, svc.Repos.Candles, svc.Repos.Signals, svc.Repos.Orders, svc.Repos.Fills, svc.Repos.Positions, svc.Repos.PnLs, svc.Repos.Events, processorMgr)

	return &BotInitResult{
		Bot:             tradingBot,
		RiskManager:     riskMgr,
		ProcessorMgr:    processorMgr,
		Balance:         balance,
		HasOpenPosition: hasOpenPosition,
	}
}
