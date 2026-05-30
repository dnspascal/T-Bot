package main

import (
	"log"
	"log/slog"
	"time"

	"github.com/denismgaya/t-bot/internal/api"
	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/provider"
	"github.com/denismgaya/t-bot/internal/provider/binance"
	"github.com/denismgaya/t-bot/internal/provider/ctrader"
)

func main() {
	setupLogging()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := setupGracefulShutdown()
	defer cancel()

	svc, err := initServices(ctx, cfg)
	if err != nil {
		log.Fatal("init services:", err)
	}
	defer svc.DB.Close()

	symbolUUID, err := svc.Lookup.Get(cfg.Symbol)
	if err != nil {
		log.Fatal("get symbol uuid:", err)
	}
	cfg.SymbolUUID = symbolUUID
	slog.Info("loaded symbol lookup", "symbol", cfg.Symbol, "symbolId", symbolUUID)

	botStart := time.Now()

	svc.Repos.Events.Insert(ctx, "started", map[string]any{
		"symbol": cfg.Symbol,
		"mode":   cfg.Mode(),
	}, 0)

	// Select and setup provider
	var prov provider.Provider

	switch cfg.Provider {
	case "binance":
		prov = binance.New(cfg, svc.DB.Pool, svc.Repos.Events, svc.Repos.Snapshots)
	default: // default to ctrader
		ctraderClient := api.NewClient(cfg.Demo, cfg.AccountID, cfg.SymbolID)
		if err := ctraderClient.Connect(); err != nil {
			log.Fatal("ctrader connect:", err)
		}
		prov = ctrader.New(cfg, ctraderClient, svc.DB.Pool, svc.Repos.Events, svc.Repos.Snapshots)
	}

	// Authenticate with provider
	authResult, err := prov.Auth(ctx)
	if err != nil {
		log.Fatal(prov.Name()+" auth:", err)
	}
	defer prov.Close()

	// Provider-specific setup (subscriptions, etc)
	if err := prov.Setup(); err != nil {
		log.Fatal(prov.Name()+" setup:", err)
	}

	// Initialize bot with all dependencies (risk manager, warmup, processors)
	botResult := initializeBot(ctx, cfg, svc, prov, authResult.Balance, authResult.HasOpenPosition)

	slog.Info("bot running",
		"symbol", cfg.Symbol,
		"provider", prov.Name(),
		"demo", cfg.Demo,
		"riskPercent", cfg.RiskPercent,
		"maxDailyLoss", cfg.MaxDailyLoss,
		"startupMs", elapsed(botStart),
	)

	botResult.Bot.Run(ctx, botStart)
}

func elapsed(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}
