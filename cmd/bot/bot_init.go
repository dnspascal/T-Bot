package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"

	"github.com/denismgaya/t-bot/internal/bot"
	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/marketstate"
	"github.com/denismgaya/t-bot/internal/notify"
	"github.com/denismgaya/t-bot/internal/provider"
	"github.com/denismgaya/t-bot/internal/risk"
	"github.com/denismgaya/t-bot/internal/strategy"
	combined "github.com/denismgaya/t-bot/internal/strategy/combined"
	"github.com/denismgaya/t-bot/internal/strategy/regime"
	srbounce "github.com/denismgaya/t-bot/internal/strategy/sr_bounce"
	 "github.com/denismgaya/t-bot/internal/strategy/trend_follow"
)

type BotInitResult struct {
	Bot          *bot.Bot
	RiskManager  *risk.Manager
	ProcessorMgr *marketstate.ProcessorManager
	Balance      float64
}

func initializeBot(ctx context.Context, cfg *config.Config, svc *Services, prov provider.Provider, symbol string, symbolUUID string, authResult *provider.AuthResult, dispatcher notify.Dispatcher) *BotInitResult {
	balance := authResult.Balance

	todayLoss, err := svc.Repos.PnLs.Today(ctx, symbolUUID)
	if err != nil {
		log.Fatal("load daily pnl:", err)
	}
	riskMgr := risk.New(cfg.RiskPercent, cfg.MaxDailyLossPct)
	switch prov.Name() {
	case "ctrader":
		// cTrader API: 100,000 units = 1 micro lot (0.01 lots). Matches V1.
		// pipValue=0.10: 1 pip on 0.01 lots EURUSD = $0.10/pip.
		// min=100,000 (1 micro lot), max=5,000,000 (50 micro lots = 0.5 lots).
		riskMgr.SetVolumeConfig(100_000, 100_000, 5_000_000, 0.10)
	case "binance":
		// unitsPerMicroLot=100_000 satoshis (0.001 BTC per micro lot).
		// pipValue=1e-7: 0.0001 price move × 0.001 BTC = $0.0000001/pip/micro-lot.
		// minVolume=100_000 satoshis (0.001 BTC) — the Binance futures LOT_SIZE minimum;
		// anything smaller floors to qty=0 in the %.3f format used by PlaceMarketOrder.
		riskMgr.SetVolumeConfig(100_000, 100_000, 5_000_000, 1e-7)
	}
	riskMgr.RestorePnL(todayLoss)

	processorMgr := marketstate.NewProcessorManager(symbolUUID, prov.Name(), svc.Repos.MarketState)
	if cfg.DevMode {
		processorMgr.SkipWarmup("H4", "D1")
		slog.Info("dev mode: H4 and D1 warmup not required", "provider", prov.Name())
	}

	for _, period := range config.TradingPeriods {
		buf := marketstate.NewMemoryCandleBuffer(100)
		proc := marketstate.NewProcessor(symbolUUID, prov.Name(), period, buf, svc.Repos.MarketState)
		processorMgr.AddProcessor(period, proc)
	}
	pipSize, err := svc.Lookup.GetPipSize(symbol)
	if err != nil {
		log.Fatal("get pip size:", err)
	}

	var lotUnit int64 = 100_000 // default: EURUSD micro lot in cTrader API units
	if prov.Name() == "ctrader" && symbol == "XAUUSD" {
		lotUnit = 100 // gold micro lot in cTrader API units
	}

	strat, err := buildStrategy(cfg.Strategy)
	if err != nil {
		log.Fatal("build strategy:", err)
	}
	slog.Info("strategy loaded", "name", strat.Name(), "provider", prov.Name())

	tradingBot := bot.New(cfg, prov, strat, symbol, symbolUUID, authResult.AccountID, pipSize, lotUnit, svc.DB.Pool, riskMgr, balance, authResult.Leverage, svc.Lookup, svc.Repos.Ticks, svc.Repos.Candles, svc.Repos.Signals, svc.Repos.Orders, svc.Repos.Fills, svc.Repos.Positions, svc.Repos.PnLs, svc.Repos.Events, processorMgr, dispatcher)

	return &BotInitResult{
		Bot:          tradingBot,
		RiskManager:  riskMgr,
		ProcessorMgr: processorMgr,
		Balance:      balance,
	}
}

func buildStrategy(name string) (strategy.Strategy, error) {
	switch name {
	case "", "regime":
		return regime.New(), nil
	case "sr_bounce":
		return srbounce.New(), nil
	case "trend_follow":
		return trendfollow.New(), nil
	case "combined":
		return combined.New(trendfollow.New(), srbounce.New()), nil
	default:
		return nil, fmt.Errorf("unknown strategy %q — valid options: regime, sr_bounce", name)
	}
}
