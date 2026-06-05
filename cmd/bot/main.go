package main

import (
	"context"
	"log"
	"log/slog"
	"sync"
	"time"

	"github.com/denismgaya/t-bot/internal/provider/ctrader/api"
	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/marketstate"
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

	botStart := time.Now()

	svc.Repos.Events.Insert(ctx, "started", map[string]any{
		"enableCTrader": cfg.EnableCTrader,
		"enableBinance": cfg.EnableBinance,
	}, 0)

	provMgr := provider.NewManager()
	var enabledProviders []string

	if cfg.EnableCTrader {
		enabledProviders = append(enabledProviders, "ctrader")

		ctraderClient := api.NewClient(cfg.CTrader.Demo, cfg.CTrader.AccountID, cfg.CTrader.SymbolID)
		if err := ctraderClient.Connect(); err != nil {
			log.Fatal("ctrader connect:", err)
		}
		ctraderProv := ctrader.New(cfg, ctraderClient, svc.DB.Pool, svc.Repos.Events, svc.Repos.Snapshots)
		if err := provMgr.Register("ctrader", ctraderProv); err != nil {
			log.Fatal("register ctrader:", err)
		}
	}

	if cfg.EnableBinance {
		enabledProviders = append(enabledProviders, "binance")

		binanceProv := binance.New(cfg, svc.DB.Pool, svc.Repos.Events, svc.Repos.Snapshots)
		if err := binanceProv.Connect(); err != nil {
			log.Fatal("binance connect:", err)
		}
		if err := provMgr.Register("binance", binanceProv); err != nil {
			log.Fatal("register binance:", err)
		}
	}

	if len(enabledProviders) == 0 {
		log.Fatal("no providers enabled")
	}

	authResults, err := provMgr.AuthAllProviders(ctx)
	if err != nil {
		slog.Warn("some providers failed auth", "err", err)
	}

	if err := provMgr.SetupAllProviders(ctx); err != nil {
		slog.Warn("some providers failed setup", "err", err)
	}


	var wg sync.WaitGroup

	if cfg.EnableCTrader {
		prov, _ := provMgr.GetProvider("ctrader")
		authResult := authResults["ctrader"]

		wg.Go(func() {
			startBotForProvider(ctx, cfg, svc, prov, cfg.CTraderSymbol, authResult, botStart)
		})
	}

	if cfg.EnableBinance {
		prov, _ := provMgr.GetProvider("binance")
		authResult := authResults["binance"]

		wg.Go(func() {
			startBotForProvider(ctx, cfg, svc, prov, cfg.BinanceSymbol, authResult, botStart)
		})
	}

	wg.Wait()
	slog.Info("all bots stopped")
}

func startBotForProvider(
	ctx context.Context,
	cfg *config.Config,
	svc *Services,
	prov provider.Provider,
	symbol string,
	authResult *provider.AuthResult,
	botStart time.Time,
) {

	defer func() {
		if r := recover(); r != nil {
			slog.Error("bot panic recovered", "provider", prov.Name(), "symbol", symbol, "panic", r)
		}
	}()

	if authResult == nil {
		slog.Error("auth result missing — provider auth failed, bot will not start", "provider", prov.Name(), "symbol", symbol)
		return
	}

	symbolUUID, err := svc.Lookup.Get(symbol)
	if err != nil {
		slog.Error("get symbol uuid failed", "provider", prov.Name(), "symbol", symbol, "err", err)
		return
	}

	botResult := initializeBot(ctx, cfg, svc, prov, symbol, symbolUUID, authResult)

	warmer := marketstate.NewWarmer(prov, botResult.ProcessorMgr, 30)
	if err := warmer.WarmupAllTimeframes(ctx, symbol); err != nil {
		slog.Error("warm-up failed — bot will not start", "provider", prov.Name(), "err", err)
		return
	}

	if err := prov.StartStreaming(); err != nil {
		slog.Error("start streaming failed", "provider", prov.Name(), "err", err)
		return
	}

	slog.Info("bot running",
		"provider", prov.Name(),
		"symbol", symbol,
		"balance", authResult.Balance,
		"riskPercent", cfg.RiskPercent,
		"maxDailyLoss", cfg.MaxDailyLoss,
		"startupMs", elapsed(botStart),
	)

	botResult.Bot.Run(ctx, botStart)
}

func elapsed(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}
