package bot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/denismgaya/t-bot/internal/api"
	"github.com/denismgaya/t-bot/internal/candle"
	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/event"
	"github.com/denismgaya/t-bot/internal/fill"
	"github.com/denismgaya/t-bot/internal/order"
	"github.com/denismgaya/t-bot/internal/pnl"
	"github.com/denismgaya/t-bot/internal/position"
	"github.com/denismgaya/t-bot/internal/risk"
	"github.com/denismgaya/t-bot/internal/signal"
	"github.com/denismgaya/t-bot/internal/strategy"
	"github.com/denismgaya/t-bot/internal/tick"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pipSize = 0.0001

type Bot struct {
	cfg          *config.Config
	client       *api.Client
	riskMgr      *risk.Manager
	strat        *strategy.CombinedStrategy
	currentPrice api.PriceEvent


	balanceMu sync.Mutex
	balance   float64

	hasOpenPosition bool // a position is live at the broker right now
	pendingOrder    bool // we sent an order, waiting for fill or rejection

	pendingOrderID         string
	pendingOrderSentAt     time.Time
	pendingSide            string // "BUY" | "SELL"
	openProviderPositionID string // cTrader positionId, set on open fill

	lastCandleOpenTime int64
	lastBar            api.Trendbar

	db        *pgxpool.Pool
	ticks     *tick.Repository
	candles   *candle.Repository
	signals   *signal.Repository
	orders    *order.Repository
	fills     *fill.Repository
	positions *position.Repository
	pnls      *pnl.Repository
	events    *event.Repository
}

func New(
	cfg *config.Config,
	client *api.Client,
	db *pgxpool.Pool,
	riskMgr *risk.Manager,
	strat *strategy.CombinedStrategy,
	balance float64,
	hasOpenPosition bool,
	ticks *tick.Repository,
	candles *candle.Repository,
	signals *signal.Repository,
	orders *order.Repository,
	fills *fill.Repository,
	positions *position.Repository,
	pnls *pnl.Repository,
	events *event.Repository,
) *Bot {
	return &Bot{
		cfg:             cfg,
		client:          client,
		db:              db,
		riskMgr:         riskMgr,
		strat:           strat,
		balance:         balance,
		hasOpenPosition: hasOpenPosition,
		ticks:           ticks,
		candles:         candles,
		signals:         signals,
		orders:          orders,
		fills:           fills,
		positions:       positions,
		pnls:            pnls,
		events:          events,
	}
}

func (b *Bot) Run(ctx context.Context, startedAt time.Time) {
	go b.tokenRefresher(ctx)
	
	for {
		select {
		case <-ctx.Done():
			b.events.Insert(context.Background(), "stopped", map[string]any{
				"uptime_ms": ms(startedAt),
			}, ms(startedAt))
			slog.Info("shutdown complete", "uptimeMs", ms(startedAt))
			return

		case price := <-b.client.PriceCh:
			b.onTick(ctx, price)

		case bar := <-b.client.TrendbarCh:
			b.onTrendbar(ctx, bar)

		case exec := <-b.client.ExecutionCh:
			b.onExecution(ctx, exec)

		case <-b.client.Dead():
			slog.Error("cTrader connection lost — exiting for systemd restart")
			os.Exit(1)
		}
	}
}

func (b *Bot) onExecution(ctx context.Context, exec api.ExecutionEvent) {
	switch exec.Type {
	case "ORDER_ACCEPTED":

	case "ORDER_FILLED":
		if b.pendingOrder {
			b.pendingOrder = false
			b.hasOpenPosition = true
			b.recordOpenFill(ctx, exec)
			slog.Info("order filled — position is open",
				"dealID", exec.Deal.DealID,
				"positionID", exec.Deal.PositionID,
				"executionPrice", exec.Deal.ExecutionPrice,
			)
			b.events.Insert(ctx, "position_opened", map[string]any{
				"deal_id":    exec.Deal.DealID,
				"position_id": exec.Deal.PositionID,
				"price":      exec.Deal.ExecutionPrice,
			}, 0)
		} else if b.hasOpenPosition {
			b.hasOpenPosition = false
			b.recordCloseFill(ctx, exec)
			slog.Info("position closed by SL/TP — refreshing balance",
				"dealID", exec.Deal.DealID,
				"grossProfit", exec.Deal.Close.GrossProfit,
			)
			b.events.Insert(ctx, "position_closed", map[string]any{
				"deal_id":      exec.Deal.DealID,
				"gross_profit": exec.Deal.Close.GrossProfit,
				"balance":      exec.Deal.Close.Balance,
			}, 0)
			go b.refreshBalance()
		}

	case "ORDER_REJECTED", "ORDER_CANCELLED", "ORDER_EXPIRED":
		b.pendingOrder = false
		slog.Warn("order not filled", "reason", exec.Type)
		b.events.Insert(ctx, "order_not_filled", map[string]any{"reason": exec.Type}, 0)

	case "ORDER_PARTIAL_FILL":
		// For market orders cTrader sends a final ORDER_FILLED after partial fills,
		// so we keep pendingOrder=true and let that event drive the state change.
		slog.Info("partial fill received — waiting for full fill",
			"dealID", exec.Deal.DealID,
			"filledVolume", exec.Deal.FilledVolume,
		)
	}
}

func (b *Bot) recordOpenFill(ctx context.Context, exec api.ExecutionEvent) {
	if !exec.HasDeal {
		return
	}
	deal := exec.Deal
	roundTripMs := time.Since(b.pendingOrderSentAt).Milliseconds()
	provOrderID := fmt.Sprintf("%d", deal.OrderID)
	provPosID := fmt.Sprintf("%d", deal.PositionID)

	if err := b.orders.UpdateExecution(ctx,
		b.pendingOrderID, provOrderID, provPosID,
		deal.ExecutionPrice, 0, "filled",
		exec.Timestamp, roundTripMs,
	); err != nil {
		slog.Error("orders.UpdateExecution failed", "err", err)
	}

	openTime := deal.ExecTime
	if err := b.positions.Upsert(ctx, position.Position{
		OurOrderID:         &b.pendingOrderID,
		Provider:           "ctrader",
		ProviderPositionID: provPosID,
		ProviderAcctID:     fmt.Sprintf("%d", b.cfg.AccountID),
		SymbolID:           b.cfg.SymbolID,
		Symbol:             b.cfg.Symbol,
		Side:               b.pendingSide,
		Volume:             deal.FilledVolume,
		OpenPrice:          &deal.ExecutionPrice,
		Status:             "open",
		OpenTimestamp:      &openTime,
	}); err != nil {
		slog.Error("positions.Upsert (open) failed", "err", err)
	}

	volume := deal.Volume
	filledVolume := deal.FilledVolume
	commission := deal.Commission
	if err := b.fills.Insert(ctx, fill.Fill{
		OurOrderID:         &b.pendingOrderID,
		Provider:           "ctrader",
		ProviderFillID:     fmt.Sprintf("%d", deal.DealID),
		ProviderOrderID:    &provOrderID,
		ProviderPositionID: &provPosID,
		SymbolID:           b.cfg.SymbolID,
		Symbol:             b.cfg.Symbol,
		Side:               b.pendingSide,
		Volume:             &volume,
		FilledVolume:       &filledVolume,
		ExecutionPrice:     &deal.ExecutionPrice,
		EventType:          "open",
		Commission:         &commission,
		ProviderCreateTime: &deal.CreateTime,
		ProviderExecTime:   &deal.ExecTime,
		ReceivedAt:         exec.Timestamp,
	}); err != nil {
		slog.Error("fills.Insert (open) failed", "err", err)
	}

	b.openProviderPositionID = provPosID
}

func (b *Bot) recordCloseFill(ctx context.Context, exec api.ExecutionEvent) {
	if !exec.HasDeal || !exec.Deal.IsClose {
		return
	}
	deal := exec.Deal
	cl := deal.Close
	provOrderID := fmt.Sprintf("%d", deal.OrderID)
	provPosID := b.openProviderPositionID

	if err := b.positions.Close(ctx, provPosID, deal.ExecTime); err != nil {
		slog.Error("positions.Close failed", "err", err)
	}

	closeSide := "SELL"
	if deal.TradeSide == api.TradeSideBuy {
		closeSide = "BUY"
	}
	volume := deal.Volume
	filledVolume := deal.FilledVolume
	closedVolume := cl.ClosedVolume
	entryPrice := cl.EntryPrice
	grossProfit := cl.GrossProfit
	swap := cl.Swap
	closeCommission := cl.Commission
	balanceAfter := cl.Balance
	pnlFee := cl.PnLConversionFee
	dealCommission := deal.Commission

	if err := b.fills.Insert(ctx, fill.Fill{
		Provider:           "ctrader",
		ProviderFillID:     fmt.Sprintf("%d", deal.DealID),
		ProviderOrderID:    &provOrderID,
		ProviderPositionID: &provPosID,
		SymbolID:           b.cfg.SymbolID,
		Symbol:             b.cfg.Symbol,
		Side:               closeSide,
		Volume:             &volume,
		FilledVolume:       &filledVolume,
		ExecutionPrice:     &deal.ExecutionPrice,
		EventType:          "close",
		Commission:         &dealCommission,
		CloseEntryPrice:    &entryPrice,
		GrossProfit:        &grossProfit,
		CloseSwap:          &swap,
		CloseCommission:    &closeCommission,
		BalanceAfter:       &balanceAfter,
		ClosedVolume:       &closedVolume,
		PnLConversionFee:   &pnlFee,
		ProviderCreateTime: &deal.CreateTime,
		ProviderExecTime:   &deal.ExecTime,
		ReceivedAt:         exec.Timestamp,
	}); err != nil {
		slog.Error("fills.Insert (close) failed", "err", err)
	}

	realized := cl.GrossProfit + cl.Commission + cl.Swap
	isWin := realized > 0
	if err := b.pnls.Upsert(ctx, b.cfg.Symbol, realized, cl.GrossProfit, cl.Commission, cl.Swap, isWin, 0, 0); err != nil {
		slog.Error("pnls.Upsert failed", "err", err)
	}

	if realized < 0 {
		b.riskMgr.RecordLoss(-realized)
	}

	b.openProviderPositionID = ""
	b.pendingOrderID = ""
	b.pendingSide = ""
}

func (b *Bot) refreshBalance() {
	info, err := b.client.FetchAccountInfo()
	if err != nil {
		slog.Error("balance refresh failed", "err", err)
		return
	}
	b.balanceMu.Lock()
	b.balance = info.Balance
	b.balanceMu.Unlock()
	slog.Info("balance refreshed", "balance", info.Balance)
}

func (b *Bot) getBalance() float64 {
	b.balanceMu.Lock()
	defer b.balanceMu.Unlock()
	return b.balance
}

func (b *Bot) onTick(ctx context.Context, price api.PriceEvent) {
	b.ticks.Insert(ctx, tick.Tick{
		Symbol:       b.cfg.Symbol,
		SymbolID:     b.cfg.SymbolID,
		Bid:          price.Bid,
		Ask:          price.Ask,
		ReceivedAt:   price.Timestamp,
		ProcessingMs: ms(price.Timestamp),
	})
	b.currentPrice = price
}

func (b *Bot) onTrendbar(ctx context.Context, bar api.Trendbar) {
	if bar.OpenTime != b.lastCandleOpenTime {
		if b.lastCandleOpenTime != 0 {
			b.processCandleClose(ctx, b.lastBar)
		}
		b.lastCandleOpenTime = bar.OpenTime
	}
	b.lastBar = bar
}

func (b *Bot) processCandleClose(ctx context.Context, bar api.Trendbar) {
	candleReceived := time.Now()
	c := strategy.Candle{
		OpenTime: time.Unix(bar.OpenTime, 0).UTC(),
		Open:     bar.Open,
		High:     bar.High,
		Low:      bar.Low,
		Close:    bar.Close,
	}
	dec := b.strat.AddCandle(c)
	b.onCandle(ctx, dec, b.currentPrice, ms(candleReceived))
}

func (b *Bot) onCandle(ctx context.Context, dec strategy.Decision, price api.PriceEvent, processingMs int64) {
	b.candles.Upsert(ctx, candle.Candle{
		Symbol:     b.cfg.Symbol,
		SymbolID:   b.cfg.SymbolID,
		Period:     "M5",
		Open:       dec.Candle.Open,
		High:       dec.Candle.High,
		Low:        dec.Candle.Low,
		Close:      dec.Candle.Close,
		BarTime:    dec.Candle.OpenTime,
		ReceivedAt: time.Now(),
	})

	signalID, err := b.signals.Insert(ctx, signal.Signal{
		Symbol:       b.cfg.Symbol,
		Signal:       dec.Signal.String(),
		FastEMA:      dec.FastEMA,
		SlowEMA:      dec.SlowEMA,
		RSI:          dec.RSI,
		Confluence:   int(dec.Confluence),
		PriceMid:     dec.Candle.Close,
		ProcessingMs: processingMs,
	})
	if err != nil {
		slog.Error("insert signal failed", "err", err)
	}

	slog.Info("candle closed",
		"signal", dec.Signal.String(),
		"confluence", dec.Confluence,
		"fastEMA", fmt.Sprintf("%.5f", dec.FastEMA),
		"slowEMA", fmt.Sprintf("%.5f", dec.SlowEMA),
		"rsi", fmt.Sprintf("%.2f", dec.RSI),
		"inSession", dec.InSession,
		"candleClose", dec.Candle.Close,
	)

	if dec.Signal == strategy.Hold {
		return
	}

	b.onTradeSignal(ctx, dec, price, signalID)
}

func (b *Bot) onTradeSignal(ctx context.Context, dec strategy.Decision, price api.PriceEvent, signalID string) {
	if b.hasOpenPosition || b.pendingOrder {
		slog.Info("signal skipped — position already active",
			"hasOpenPosition", b.hasOpenPosition,
			"pendingOrder", b.pendingOrder,
		)
		return
	}

	if !b.riskMgr.CanTrade() {
		slog.Warn("daily loss limit hit — bot paused",
			"dailyLoss", b.riskMgr.DailyLoss(),
			"limit", b.cfg.MaxDailyLoss,
		)
		b.events.Insert(ctx, "daily_limit_hit", map[string]any{
			"daily_loss": b.riskMgr.DailyLoss(),
			"limit":      b.cfg.MaxDailyLoss,
		}, 0)
		return
	}

	volume, err := b.riskMgr.PositionSize(b.getBalance(), b.cfg.StopLossPips)
	if err != nil {
		slog.Warn("position size error", "err", err)
		return
	}
	
	if dec.Confluence == strategy.ConfluenceWeak {
		volume = 100000 
	}

	var side uint32
	var sl, tp float64

	if dec.Signal == strategy.Buy {
		side = api.TradeSideBuy
		sl = price.Ask - b.cfg.StopLossPips*pipSize
		tp = price.Ask + b.cfg.TakeProfitPips*pipSize
	} else {
		side = api.TradeSideSell
		sl = price.Bid + b.cfg.StopLossPips*pipSize
		tp = price.Bid - b.cfg.TakeProfitPips*pipSize
	}

	sideStr := sideString(dec.Signal)
	sentAt := time.Now()

	orderID, err := b.orders.Insert(ctx, order.Order{
		SignalID:  &signalID,
		Provider:  "ctrader",
		Symbol:    b.cfg.Symbol,
		SymbolID:  b.cfg.SymbolID,
		Side:      sideStr,
		Volume:    volume,
		SL:        &sl,
		TP:        &tp,
		SentAt:    &sentAt,
	})
	if err != nil {
		slog.Error("insert order record failed", "err", err)
	}

	if err := b.client.PlaceMarketOrder(side, volume, sl, tp); err != nil {
		slog.Error("order failed", "err", err, "elapsedMs", ms(sentAt))
		b.orders.UpdateError(ctx, orderID, "SEND_FAILED", err.Error())
		b.events.Insert(ctx, "error", map[string]any{
			"error":      err.Error(),
			"stage":      "place_order",
			"side":       sideStr,
			"volume":     volume,
			"elapsed_ms": ms(sentAt),
		}, ms(sentAt))
	} else {
		b.pendingOrder = true
		b.pendingOrderID = orderID
		b.pendingOrderSentAt = sentAt
		b.pendingSide = sideStr
		b.events.Insert(ctx, "order_sent", map[string]any{
			"order_id":   orderID,
			"signal_id":  signalID,
			"side":       sideStr,
			"volume":     volume,
			"confluence": dec.Confluence,
			"rsi":        dec.RSI,
			"sl":         sl,
			"tp":         tp,
			"elapsed_ms": ms(sentAt),
		}, ms(sentAt))
	}
}

func (b *Bot) tokenRefresher(ctx context.Context) {
	ticker := time.NewTicker(25 * 24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			newAccess, newRefresh, err := api.RefreshToken(
				b.cfg.ClientID,
				b.cfg.ClientSecret,
				b.cfg.RefreshToken,
			)
			if err != nil {
				slog.Error("cTrader token refresh failed", "err", err)
				continue
			}
			b.cfg.AccessToken = newAccess
			b.cfg.RefreshToken = newRefresh
			if err := saveCredential(ctx, b.db, "ctrader_access_token", newAccess); err != nil {
				slog.Error("save access token failed", "err", err)
			}
			if err := saveCredential(ctx, b.db, "ctrader_refresh_token", newRefresh); err != nil {
				slog.Error("save refresh token failed", "err", err)
			}
			slog.Info("cTrader access token refreshed successfully")
		}
	}
}

func saveCredential(ctx context.Context, db *pgxpool.Pool, key, value string) error {
	_, err := db.Exec(ctx, `
		INSERT INTO bot_credentials (key, value)
		VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()
	`, key, value)
	return err
}

func LoadCredential(ctx context.Context, db *pgxpool.Pool, key string) (string, error) {
	var value string
	err := db.QueryRow(ctx, "SELECT value FROM bot_credentials WHERE key = $1", key).Scan(&value)
	return value, err
}

func ms(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}

func sideString(sig strategy.Signal) string {
	if sig == strategy.Buy {
		return "BUY"
	}
	return "SELL"
}
