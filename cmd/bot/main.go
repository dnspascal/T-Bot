package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	ossignal "os/signal"
	"syscall"
	"time"

	"github.com/denismgaya/t-bot/internal/api"
	"github.com/denismgaya/t-bot/internal/bot"
	"github.com/denismgaya/t-bot/internal/candle"
	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/database"
	"github.com/denismgaya/t-bot/internal/event"
	"github.com/denismgaya/t-bot/internal/fill"
	"github.com/denismgaya/t-bot/internal/order"
	"github.com/denismgaya/t-bot/internal/pnl"
	"github.com/denismgaya/t-bot/internal/position"
	"github.com/denismgaya/t-bot/internal/risk"
	"github.com/denismgaya/t-bot/internal/signal"
	"github.com/denismgaya/t-bot/internal/snapshot"
	"github.com/denismgaya/t-bot/internal/strategy"
	"github.com/denismgaya/t-bot/internal/tick"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := ossignal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := database.New(ctx, cfg.DatabaseURL, 10, 2)
	if err != nil {
		log.Fatal("database:", err)
	}
	defer pool.Close()

	ticks     := tick.New(pool)
	candles   := candle.New(pool)
	signals   := signal.New(pool)
	orders    := order.New(pool)
	fills     := fill.New(pool)
	positions := position.New(pool)
	pnls      := pnl.New(pool)
	events    := event.New(pool)
	snapshots := snapshot.New(pool)

	botStart := time.Now()

	
	events.Insert(ctx, "started", map[string]any{
		"symbol": cfg.Symbol,
		"mode":   cfg.Mode(),
	}, 0)

	// Restore daily P&L so risk manager survives restarts
	todayLoss, err := pnls.Today(ctx, cfg.Symbol)
	if err != nil {
		log.Fatal("load daily pnl:", err)
	}
	riskMgr := risk.New(cfg.RiskPercent, cfg.MaxDailyLoss)
	if todayLoss < 0 {
		riskMgr.RestoreLoss(-todayLoss)
	}
	slog.Info("daily pnl restored", "todayLoss", todayLoss)

	strat := strategy.NewCombinedStrategy(9, 21, 14)

	// Load persisted tokens from DB (overrides .env after first refresh)
	if token, err := bot.LoadCredential(ctx, pool, "ctrader_access_token"); err == nil && token != "" {
		cfg.AccessToken = token
		slog.Info("loaded cTrader access token from DB")
	}
	if token, err := bot.LoadCredential(ctx, pool, "ctrader_refresh_token"); err == nil && token != "" {
		cfg.RefreshToken = token
	}

	// Connect to cTrader
	connectStart := time.Now()
	client := api.NewClient(cfg.Demo, cfg.AccountID, cfg.SymbolID)

	if err := client.Connect(); err != nil {
		events.Insert(ctx, "error", map[string]any{"error": err.Error(), "stage": "connect"}, elapsed(connectStart))
		log.Fatal("connect:", err)
	}
	events.Insert(ctx, "connected", map[string]any{"host": cfg.Symbol}, elapsed(connectStart))

	authStart := time.Now()
	if err := client.AuthApp(cfg.ClientID, cfg.ClientSecret); err != nil {
		events.Insert(ctx, "auth_fail", map[string]any{"error": err.Error(), "stage": "app_auth"}, elapsed(authStart))
		log.Fatal("app auth:", err)
	}
	time.Sleep(2 * time.Second)

	// Discover ctidTraderAccountId — this is cTrader's internal ID, different from the broker account number.
	accounts, err := client.GetAccountList(cfg.AccessToken)
	if err != nil {
		log.Fatal("get account list:", err)
	}
	var ctidAccountID int64
	for _, acc := range accounts {
		if acc.IsLive == !cfg.Demo {
			ctidAccountID = acc.CtidTraderAccountID
			slog.Info("found trading account",
				"ctidTraderAccountID", acc.CtidTraderAccountID,
				"traderLogin", acc.TraderLogin,
				"isLive", acc.IsLive,
			)
			break
		}
	}
	if ctidAccountID == 0 {
		log.Fatalf("no %s account found in account list (got %d accounts)", cfg.Mode(), len(accounts))
	}
	client.SetAccountID(ctidAccountID)

	if err := client.AuthAccount(cfg.AccessToken); err != nil {
		events.Insert(ctx, "auth_fail", map[string]any{"error": err.Error(), "stage": "account_auth"}, elapsed(authStart))
		log.Fatal("account auth:", err)
	}
	events.Insert(ctx, "auth_ok", map[string]any{"account_id": cfg.AccountID}, elapsed(authStart))
	time.Sleep(2 * time.Second)

	// Fetch real account balance and snapshot it
	fetchStart := time.Now()
	traderInfo, err := client.FetchAccountInfo()
	if err != nil {
		slog.Warn("FetchAccountInfo failed, using configured initial balance", "err", err, "balance", cfg.InitialBalance)
		traderInfo = api.TraderInfo{Balance: cfg.InitialBalance}
	}
	balance    := traderInfo.Balance
	leverage   := traderInfo.Leverage
	brokerName := traderInfo.BrokerName
	trigger    := "startup"
	snapshots.Insert(ctx, snapshot.Snapshot{
		Provider:       "ctrader",
		ProviderAcctID: fmt.Sprintf("%d", cfg.AccountID),
		Balance:        balance,
		LeverageRatio:  &leverage,
		BrokerName:     &brokerName,
		Trigger:        &trigger,
		SnapshottedAt:  time.Now(),
	})
	events.Insert(ctx, "account_snapshot", map[string]any{
		"balance":  balance,
		"leverage": leverage,
		"broker":   brokerName,
	}, elapsed(fetchStart))

	// Reconcile: discover any positions already open from before a restart
	reconcileStart := time.Now()
	openPositions, err := client.Reconcile()
	if err != nil {
		log.Fatal("reconcile:", err)
	}
	// check if any open position exists for our symbol specifically
	var hasOpenPosition bool
	for _, pos := range openPositions {
		if pos.SymbolID == cfg.SymbolID {
			hasOpenPosition = true
			break
		}
	}
	slog.Info("startup reconcile", "openPositions", len(openPositions), "hasOpenPosition", hasOpenPosition)
	events.Insert(ctx, "reconcile", map[string]any{
		"open_positions":    len(openPositions),
		"has_open_position": hasOpenPosition,
	}, elapsed(reconcileStart))

	// Warm up EMA + RSI from cTrader historical trendbars (50 × M5 candles ≈ 4 hours)
	warmupStart := time.Now()
	historicalBars, err := client.FetchHistoricalTrendbars(50)
	if err != nil {
		slog.Warn("warmup fetch failed, starting cold", "err", err)
	} else {
		closePrices := make([]float64, len(historicalBars))
		for i, bar := range historicalBars {
			closePrices[i] = bar.Close
			candles.Upsert(ctx, candle.Candle{
				Symbol:     cfg.Symbol,
				SymbolID:   cfg.SymbolID,
				Period:     "M5",
				Open:       bar.Open,
				High:       bar.High,
				Low:        bar.Low,
				Close:      bar.Close,
				TickVolume: bar.Volume,
				BarTime:    time.Unix(bar.OpenTime, 0).UTC(),
				ReceivedAt: time.Now(),
			})
		}
		strat.WarmUp(closePrices)
		slog.Info("strategy warmed up", "candles", len(historicalBars), "elapsedMs", elapsed(warmupStart))
	}

	if err := client.SubscribeSpots(); err != nil {
		log.Fatal("subscribe spots:", err)
	}
	if err := client.SubscribeLiveTrendbar(); err != nil {
		log.Fatal("subscribe live trendbar:", err)
	}

	slog.Info("bot running",
		"symbol", cfg.Symbol,
		"demo", cfg.Demo,
		"riskPercent", cfg.RiskPercent,
		"maxDailyLoss", cfg.MaxDailyLoss,
		"startupMs", elapsed(botStart),
	)

	bot.New(cfg, client, pool, riskMgr, strat, balance, hasOpenPosition, ticks, candles, signals, orders, fills, positions, pnls, events).Run(ctx, botStart)
}

func elapsed(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}
