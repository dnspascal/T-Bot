package bot

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/denismgaya/t-bot/internal/candle"
	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/event"
	"github.com/denismgaya/t-bot/internal/fill"
	"github.com/denismgaya/t-bot/internal/indicator"
	"github.com/denismgaya/t-bot/internal/marketstate"
	"github.com/denismgaya/t-bot/internal/order"
	"github.com/denismgaya/t-bot/internal/pnl"
	"github.com/denismgaya/t-bot/internal/position"
	"github.com/denismgaya/t-bot/internal/provider"
	"github.com/denismgaya/t-bot/internal/risk"
	"github.com/denismgaya/t-bot/internal/signal"
	"github.com/denismgaya/t-bot/internal/symbol"
	"github.com/denismgaya/t-bot/internal/tick"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pipSize = 0.0001

// Decision represents a trading decision
type Decision struct {
	Signal   string  // "BUY" or "SELL"
	FastEMA  float64
	SlowEMA  float64
	RSI      float64
	ADX      float64
	ATR      float64
}

type Bot struct {
	cfg       *config.Config
	provider  provider.Provider
	riskMgr   *risk.Manager
	currentPrice provider.PriceEvent

	symbol        string
	symbolUUID    string
	providerAcctID string

	balanceMu sync.Mutex
	balance   float64

	hasOpenPosition bool // a position is live at the broker right now
	pendingOrder    bool // we sent an order, waiting for fill or rejection

	pendingOrderID         string
	pendingOrderSentAt     time.Time
	pendingSide            string // "BUY" | "SELL"
	openProviderPositionID string // provider positionId, set on open fill

	lastCandleOpenTime int64
	lastCandleClose    float64

	db     *pgxpool.Pool
	lookup *symbol.SymbolLookup
	ticks     *tick.Repository
	candles   *candle.Repository
	signals   *signal.Repository
	orders    *order.Repository
	fills     *fill.Repository
	positions *position.Repository
	pnls      *pnl.Repository
	events    *event.Repository

	// Market state processing (multi-timeframe indicators)
	// Keyed by symbolID then period: marketStates[symbolID][period]
	processorMgr *marketstate.ProcessorManager
	marketStates map[string]map[string]indicator.MarketState
}

func New(
	cfg *config.Config,
	prov provider.Provider,
	symbol string,
	symbolUUID string,
	providerAcctID string,
	db *pgxpool.Pool,
	riskMgr *risk.Manager,
	balance float64,
	hasOpenPosition bool,
	lookup *symbol.SymbolLookup,
	ticks *tick.Repository,
	candles *candle.Repository,
	signals *signal.Repository,
	orders *order.Repository,
	fills *fill.Repository,
	positions *position.Repository,
	pnls *pnl.Repository,
	events *event.Repository,
	processorMgr *marketstate.ProcessorManager,
) *Bot {
	return &Bot{
		cfg:             cfg,
		provider:        prov,
		symbol:          symbol,
		symbolUUID:      symbolUUID,
		providerAcctID:  providerAcctID,
		db:              db,
		riskMgr:         riskMgr,
		balance:         balance,
		hasOpenPosition: hasOpenPosition,
		lookup:          lookup,
		ticks:           ticks,
		candles:         candles,
		signals:         signals,
		orders:          orders,
		fills:           fills,
		positions:       positions,
		pnls:            pnls,
		events:          events,
		processorMgr:    processorMgr,
		marketStates:    make(map[string]map[string]indicator.MarketState),
	}
}

func (b *Bot) Run(ctx context.Context, startedAt time.Time) {
	go b.tokenRefresher(ctx)

	b.testOrder()

	for {
		select {
		case <-ctx.Done():
			b.events.Insert(context.Background(), "stopped", map[string]any{
				"uptime_ms": ms(startedAt),
			}, ms(startedAt))
			slog.Info("shutdown complete", "uptimeMs", ms(startedAt))
			return

		case price := <-b.provider.PriceChan():
			b.onTick(ctx, price)

		case candle := <-b.provider.CandleChan():
			b.onCandleReceived(ctx, candle)

		case exec := <-b.provider.ExecutionChan():
			b.onExecution(ctx, exec)

		case <-b.provider.DisconnectedChan():
			slog.Error("provider connection lost — bot stopping")
			return
		}
	}
}

func (b *Bot) onCandleReceived(ctx context.Context, c provider.Candle) {
	b.storeCandle(ctx, c)

	states, err := b.processorMgr.ProcessCandle(ctx, c.Timeframe, c.OpenTime, c.Open, c.High, c.Low, c.Close, c.Volume)
	if err != nil {
		slog.Error("process candle failed", "timeframe", c.Timeframe, "err", err)
	}
	if b.marketStates[b.symbolUUID] == nil {
		b.marketStates[b.symbolUUID] = make(map[string]indicator.MarketState)
	}
	maps.Copy(b.marketStates[b.symbolUUID], states)

	if c.Timeframe == "M5" {
		if c.OpenTime != b.lastCandleOpenTime {
			if b.lastCandleOpenTime != 0 {
				b.processClosedCandle(ctx, b.lastCandleClose)
			}
			b.lastCandleOpenTime = c.OpenTime
		}
		b.lastCandleClose = c.Close
	}
}

func (b *Bot) onExecution(ctx context.Context, exec provider.ExecutionEvent) {
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
				"deal_id":     exec.Deal.DealID,
				"position_id": exec.Deal.PositionID,
				"price":       exec.Deal.ExecutionPrice,
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

func (b *Bot) processClosedCandle(ctx context.Context, closePrice float64) {
	candleReceived := time.Now()

	// Get current M5 market state
	m5State, ok := b.marketStates[b.symbolUUID]["M5"]
	if !ok {
		slog.Warn("M5 market state not available yet")
		return
	}

	// Check if we have enough data to trade
	if !b.processorMgr.AllWarmedUp() {
		slog.Info("warming up indicators", "warmed", false)
		return
	}

	// Use market state for signal generation
	b.onSignalCandle(ctx, m5State, b.currentPrice, ms(candleReceived))
}

func (b *Bot) recordOpenFill(ctx context.Context, exec provider.ExecutionEvent) {
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
		ProviderAcctID:     b.providerAcctID,
		SymbolID:           b.symbolUUID,
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
		SymbolID:           b.symbolUUID,
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

func (b *Bot) recordCloseFill(ctx context.Context, exec provider.ExecutionEvent) {
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
	if deal.TradeSide == 1 { // TradeSideBuy = 1
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
		SymbolID:           b.symbolUUID,
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
	if err := b.pnls.Upsert(ctx, b.symbolUUID, realized, cl.GrossProfit, cl.Commission, cl.Swap, isWin, 0, 0); err != nil {
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
	info, err := b.provider.FetchAccountInfo(context.Background())
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

func (b *Bot) onTick(ctx context.Context, price provider.PriceEvent) {
	b.ticks.Insert(ctx, tick.Tick{
		SymbolID:     b.symbolUUID,
		Bid:          price.Bid,
		Ask:          price.Ask,
		ReceivedAt:   price.Timestamp,
		ProcessingMs: ms(price.Timestamp),
	})
	b.currentPrice = price
}

// generateSignalFromMarketState creates a trading signal from market state indicators
func (b *Bot) generateSignalFromMarketState(state indicator.MarketState) string {
	// For now: simple EMA crossover logic
	// In future: add multi-timeframe confluence, RSI filters, etc.

	if state.EMAFast > state.EMASlow && state.RSI > 50 {
		// Potential BUY: fast EMA above slow, RSI confirms
		return "BUY"
	}
	if state.EMAFast < state.EMASlow && state.RSI < 50 {
		// Potential SELL: fast EMA below slow, RSI confirms
		return "SELL"
	}
	return "HOLD"
}

func (b *Bot) onSignalCandle(ctx context.Context, state indicator.MarketState, price provider.PriceEvent, processingMs int64) {
	// Generate trading signal from market state
	sig := b.generateSignalFromMarketState(state)

	// Store signal with indicator values
	signalID, err := b.signals.Insert(ctx, signal.Signal{
		SymbolID:     b.symbolUUID,
		Signal:       sig,
		FastEMA:      state.EMAFast,
		SlowEMA:      state.EMASlow,
		RSI:          state.RSI,
		Confluence:   0,  // Will calculate multi-timeframe confluence in future
		PriceMid:     state.Close,
		ProcessingMs: processingMs,
	})
	if err != nil {
		slog.Error("insert signal failed", "err", err)
	}

	slog.Info("candle closed",
		"signal", sig,
		"fastEMA", fmt.Sprintf("%.5f", state.EMAFast),
		"slowEMA", fmt.Sprintf("%.5f", state.EMASlow),
		"rsi", fmt.Sprintf("%.2f", state.RSI),
		"adx", fmt.Sprintf("%.2f", state.ADX),
		"atr", fmt.Sprintf("%.5f", state.ATR),
		"candleClose", state.Close,
	)

	if sig == "HOLD" {
		return
	}

	// Create a Decision for onTradeSignal
	dec := Decision{
		Signal:   sig,
		FastEMA:  state.EMAFast,
		SlowEMA:  state.EMASlow,
		RSI:      state.RSI,
		ADX:      state.ADX,
		ATR:      state.ATR,
	}

	b.onTradeSignal(ctx, dec, price, signalID)
}

func (b *Bot) storeCandle(ctx context.Context, c provider.Candle) {
	if err := b.candles.Upsert(ctx, candle.Candle{
		SymbolID:   b.symbolUUID,
		Period:     c.Timeframe,
		Open:       c.Open,
		High:       c.High,
		Low:        c.Low,
		Close:      c.Close,
		TickVolume: c.Volume,
		BarTime:    time.Unix(c.OpenTime, 0).UTC(),
		ReceivedAt: time.Now(),
	}); err != nil {
		slog.Error("store candle failed", "period", c.Timeframe, "err", err)
	}
}

func (b *Bot) onTradeSignal(ctx context.Context, dec Decision, price provider.PriceEvent, signalID string) {
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

	sideStr := dec.Signal
	sl := b.cfg.StopLossPips
	tp := b.cfg.TakeProfitPips

	sentAt := time.Now()

	orderID, err := b.orders.Insert(ctx, order.Order{
		SignalID: &signalID,
		Provider: b.provider.Name(),
		SymbolID: b.symbolUUID,
		Side:     sideStr,
		Volume:   volume,
		SL:       &sl,
		TP:       &tp,
		SentAt:   &sentAt,
	})
	if err != nil {
		slog.Error("insert order record failed", "err", err)
	}

	_, err = b.provider.PlaceMarketOrder(ctx, sideStr, volume, sl, tp)
	if err != nil {
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
			"rsi":        dec.RSI,
			"adx":        dec.ADX,
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
			if err := b.provider.RefreshCredentials(ctx); err != nil {
				slog.Error("credentials refresh failed", "err", err, "provider", b.provider.Name())
				continue
			}
			slog.Info("credentials refreshed successfully", "provider", b.provider.Name())
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

func sideString(sig string) string {
	return sig // Already "BUY" or "SELL"
}

func (b *Bot) testOrder() {
	_, err := b.provider.PlaceMarketOrder(context.Background(), "BUY", 100000, 10, 20)
	if err != nil {
		slog.Error("test order failed", "err", err)
		return
	}
	slog.Info("test order sent successfully")
	time.Sleep(5 * time.Second)
}
