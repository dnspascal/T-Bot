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

type Bot struct {
	cfg          *config.Config
	provider     provider.Provider
	riskMgr      *risk.Manager
	currentPrice provider.PriceEvent
	registry     *PositionRegistry

	symbol         string
	symbolUUID     string
	providerAcctID string

	balanceMu sync.Mutex
	balance   float64

	// Single in-flight order at a time.
	pendingOrder       bool
	pendingOrderID     string
	pendingOrderSentAt time.Time
	pendingSide        string
	pendingTier        int
	pendingSLPrice     float64
	pendingTPPrice     float64
	pendingATR         float64

	lastCandleOpenTime int64
	lastCandleClose    float64

	pendingCloseReasons map[string]string

	db        *pgxpool.Pool
	lookup    *symbol.SymbolLookup
	ticks     *tick.Repository
	candles   *candle.Repository
	signals   *signal.Repository
	orders    *order.Repository
	fills     *fill.Repository
	positions *position.Repository
	pnls      *pnl.Repository
	events    *event.Repository

	processorMgr *marketstate.ProcessorManager
	marketStates map[string]map[string]indicator.MarketState
}

func New(
	cfg *config.Config,
	prov provider.Provider,
	sym string,
	symbolUUID string,
	providerAcctID string,
	db *pgxpool.Pool,
	riskMgr *risk.Manager,
	balance float64,
	_ bool, // hasOpenPosition — now derived from registry
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
		cfg:            cfg,
		provider:       prov,
		symbol:         sym,
		symbolUUID:     symbolUUID,
		providerAcctID: providerAcctID,
		db:             db,
		riskMgr:        riskMgr,
		balance:        balance,
		registry:            newPositionRegistry(),
		pendingCloseReasons: make(map[string]string),
		lookup:         lookup,
		ticks:          ticks,
		candles:        candles,
		signals:        signals,
		orders:         orders,
		fills:          fills,
		positions:      positions,
		pnls:           pnls,
		events:         events,
		processorMgr:   processorMgr,
		marketStates:   make(map[string]map[string]indicator.MarketState),
	}
}

func (b *Bot) Run(ctx context.Context, startedAt time.Time) {
	b.reconcileOpenPositions(ctx)

	go b.tokenRefresher(ctx)
	if b.provider.Name() == "ctrader" {
		go b.weekendPositionCloser(ctx)
	}

	priceCh := b.provider.PriceChan()
	candleCh := b.provider.CandleChan()
	execCh := b.provider.ExecutionChan()
	discCh := b.provider.DisconnectedChan()

	if b.cfg.SendTestPosition {
		b.sendTestPosition(ctx)
	}

	for {
		select {
		case <-ctx.Done():
			b.events.Insert(context.Background(), "stopped", map[string]any{
				"uptime_ms": ms(startedAt),
			}, ms(startedAt))
			slog.Info("shutdown complete", "uptimeMs", ms(startedAt))
			return

		case price := <-priceCh:
			b.onTick(ctx, price)

		case c := <-candleCh:
			b.onCandleReceived(ctx, c)

		case exec := <-execCh:
			b.onExecution(ctx, exec)

		case <-discCh:
			slog.Error("provider connection lost — bot stopping")
			return
		}
	}
}

func (b *Bot) reconcileOpenPositions(ctx context.Context) {
	dbPositions, err := b.positions.OpenByProvider(ctx, b.provider.Name())
	if err != nil {
		slog.Error("startup reconcile: failed to query open positions", "err", err)
		return
	}
	if len(dbPositions) == 0 {
		return
	}

	brokerOpen := make(map[string]bool)
	brokerPositions, err := b.provider.ReconcilePositions(ctx)
	if err != nil {
		slog.Warn("startup reconcile: could not fetch broker positions, trusting DB",
			"provider", b.provider.Name(), "err", err,
		)
	} else {
		for _, bp := range brokerPositions {
			brokerOpen[bp.PositionID] = true
		}
	}

	loaded, purged := 0, 0
	for _, p := range dbPositions {
		if p.ProviderPositionID == "" {
			slog.Warn("startup reconcile: skipping position with empty provider ID",
				"provider", b.provider.Name(), "dbID", p.ID,
			)
			continue
		}

		// If broker confirms it's closed, mark DB and skip.
		if len(brokerOpen) > 0 && !brokerOpen[p.ProviderPositionID] {
			slog.Warn("startup reconcile: position closed at broker while bot was offline — purging",
				"provider", b.provider.Name(), "posID", p.ProviderPositionID,
			)
			if dbErr := b.positions.Close(ctx, b.provider.Name(), p.ProviderPositionID, time.Now(), nil, nil); dbErr != nil {
				slog.Error("startup reconcile: failed to mark position closed in DB", "posID", p.ProviderPositionID, "err", dbErr)
			}
			purged++
			continue
		}

		var openPrice, sl, tp float64
		if p.OpenPrice != nil {
			openPrice = *p.OpenPrice
		}
		if p.CurrentSL != nil {
			sl = *p.CurrentSL
		}
		if p.CurrentTP != nil {
			tp = *p.CurrentTP
		}
		var openTime time.Time
		if p.OpenTimestamp != nil {
			openTime = *p.OpenTimestamp
		}
		b.registry.Register(trackedPosition{
			ProviderPositionID: p.ProviderPositionID,
			Side:               p.Side,
			Volume:             p.Volume,
			OpenPrice:          openPrice,
			SLPrice:            sl,
			TPPrice:            tp,
			OpenTime:           openTime,
			Tier:               0,
		})
		slog.Info("startup reconcile: loaded open position",
			"provider", b.provider.Name(),
			"posID", p.ProviderPositionID,
			"side", p.Side,
			"openPrice", openPrice,
			"volume", p.Volume,
		)
		loaded++
	}
	slog.Info("startup reconcile complete",
		"provider", b.provider.Name(),
		"loaded", loaded,
		"purged", purged,
	)
}

func (b *Bot) onCandleReceived(ctx context.Context, c provider.Candle) {
	b.storeCandle(ctx, c)

	states, err := b.processorMgr.ProcessCandle(ctx, c.Timeframe, c.OpenTime, c.Open, c.High, c.Low, c.Close, c.Volume, c.ReceivedAt)
	if err != nil {
		slog.Error("process candle failed", "timeframe", c.Timeframe, "err", err)
	}
	if b.marketStates[b.symbolUUID] == nil {
		b.marketStates[b.symbolUUID] = make(map[string]indicator.MarketState)
	}
	maps.Copy(b.marketStates[b.symbolUUID], states)

	switch c.Timeframe {
	case "M1":
		if b.registry.Count() > 0 {
			// b.logM1State(ctx, c.Close)
		}
	case "M5":
		if c.OpenTime != b.lastCandleOpenTime {
			if b.lastCandleOpenTime != 0 {
				b.processClosedCandle(ctx, b.lastCandleClose)
			}
			b.lastCandleOpenTime = c.OpenTime
		}
		b.lastCandleClose = c.Close
	}
}

func (b *Bot) processClosedCandle(ctx context.Context, _ float64) {
	if !b.processorMgr.AllWarmedUp() {
		slog.Info("warming up indicators")
		return
	}

	states := b.marketStates[b.symbolUUID]
	m5, ok := states["M5"]
	if !ok {
		return
	}

	mid := b.currentPrice.Mid
	if mid == 0 {
		mid = (b.currentPrice.Bid + b.currentPrice.Ask) / 2
	}

	// Step 1: log unrealized P&L for every open position
	b.logUnrealizedPnL(mid)

	// Step 2: check open positions for exit conditions
	b.watchPositions(ctx, m5)

	// Step 3: evaluate entry — always insert signal for full audit trail
	evalStart := time.Now()
	result := evaluateEntry(states, mid)

	barTime := time.Unix(m5.BarTime, 0).UTC()
	signalID, err := b.signals.Insert(ctx, signal.Signal{
		SymbolID:            b.symbolUUID,
		Provider:            b.provider.Name(),
		Signal:              result.Signal,
		Confluence:          result.Confluence,
		ProcessingUS:        time.Since(evalStart).Microseconds(),
		CheckedMarketStates: buildMarketStateSnapshots(states),
		BarTime:             &barTime,
	})
	if err != nil {
		slog.Error("insert signal failed", "err", err)
	}



	if result.Signal == "HOLD" {
		return
	}

	b.onTradeSignal(ctx, result, b.currentPrice, signalID)
}

// unrealizedUSD converts a signed price difference and volume to USD P&L.
// CTrader: 100,000 API units = 1 micro lot (0.01 lots). For EURUSD: P&L = priceDiff × volume / 100.
// Binance: volume is in satoshis (100,000,000 = 1 BTC).
func (b *Bot) unrealizedUSD(priceDiff float64, volume int64) float64 {
	if b.provider.Name() == "ctrader" {
		return priceDiff * float64(volume) / 100
	}
	return priceDiff * float64(volume) / 100_000_000
}

func (b *Bot) logUnrealizedPnL(currentPrice float64) {
	positions := b.registry.All()
	if len(positions) == 0 {
		return
	}
	var totalUnrealized float64
	for _, pos := range positions {
		if pos.OpenPrice == 0 {
			continue
		}
		var unrealized float64
		if pos.Side == "BUY" {
			unrealized = b.unrealizedUSD(currentPrice-pos.OpenPrice, pos.Volume)
		} else {
			unrealized = b.unrealizedUSD(pos.OpenPrice-currentPrice, pos.Volume)
		}
		totalUnrealized += unrealized
		slog.Info("position P&L",
			"posID", pos.ProviderPositionID,
			"side", pos.Side,
			"openPrice", pos.OpenPrice,
			"currentPrice", currentPrice,
			"unrealizedUSD", fmt.Sprintf("%.2f", unrealized),
			"tier", pos.Tier,
		)
	}
	if len(positions) > 1 {
		slog.Info("total unrealized P&L", "usd", fmt.Sprintf("%.2f", totalUnrealized))
	}
}

func (b *Bot) onExecution(ctx context.Context, exec provider.ExecutionEvent) {
	if !exec.HasDeal {
		switch exec.Type {
		case "ORDER_REJECTED", "ORDER_CANCELLED", "ORDER_EXPIRED":
			b.pendingOrder = false
			slog.Warn("order not filled", "reason", exec.Type)
			b.events.Insert(ctx, "order_not_filled", map[string]any{"reason": exec.Type}, 0)
		}
		return
	}

	switch exec.Type {
	case "ORDER_FILLED":
		if exec.Deal.IsClose {
			b.recordCloseFill(ctx, exec)
			// Prefer the balance the broker includes in the close deal; only fall
			// back to FetchAccountInfo if it is missing (Binance spot, some demos).
			if exec.Deal.Close != nil && exec.Deal.Close.Balance > 0 {
				b.balanceMu.Lock()
				b.balance = exec.Deal.Close.Balance
				b.balanceMu.Unlock()
				slog.Info("balance updated from close fill", "balance", exec.Deal.Close.Balance)
			} else {
				go b.refreshBalance()
			}
		} else {
			b.pendingOrder = false
			b.recordOpenFill(ctx, exec)
		}

	case "ORDER_PARTIAL_FILL":
		slog.Info("partial fill — waiting for full fill",
			"dealID", exec.Deal.DealID,
			"filledVolume", exec.Deal.FilledVolume,
		)

	case "ORDER_REJECTED", "ORDER_CANCELLED", "ORDER_EXPIRED":
		b.pendingOrder = false
		slog.Warn("order not filled", "reason", exec.Type)
		b.events.Insert(ctx, "order_not_filled", map[string]any{"reason": exec.Type}, 0)
	}
}

func (b *Bot) onTradeSignal(ctx context.Context, result EntryResult, price provider.PriceEvent, signalID string) {
	if b.pendingOrder {
		slog.Info("signal skipped — pending order active")
		return
	}

	if ok, reason := b.registry.CanOpen(result.Tier, result.Signal); !ok {
		slog.Info("signal skipped — position limit", "reason", reason)
		return
	}

	if !b.riskMgr.CanTrade() {
		slog.Warn("daily loss limit hit — signal skipped",
			"dailyLoss", b.riskMgr.DailyLoss(),
			"limit", b.cfg.MaxDailyLoss,
		)
		b.events.Insert(ctx, "daily_limit_hit", map[string]any{
			"daily_loss": b.riskMgr.DailyLoss(),
		}, 0)
		return
	}

	volume, err := b.riskMgr.PositionSizeForTier(b.getBalance(), result.SLPips, result.Tier)
	if err != nil {
		slog.Warn("position size error", "err", err)
		return
	}

	// Binance spot: cap volume so we never try to buy more BTC than the balance can afford.
	// Without this, small balances produce orders larger than available USDT → instant rejection.
	if b.provider.Name() == "binance" {
		mid := b.currentPrice.Mid
		if mid == 0 {
			mid = (b.currentPrice.Bid + b.currentPrice.Ask) / 2
		}
		if mid > 0 {
			// Use 90% of balance to leave a small buffer for fees.
			maxAffordable := int64((b.getBalance() / mid) * 100_000_000 * 0.90)
			if volume > maxAffordable {
				slog.Info("binance: volume capped to affordable size",
					"computed", volume, "capped", maxAffordable, "balance", b.getBalance(), "price", mid,
				)
				volume = maxAffordable
			}
		}
	}

	// Compute price levels for DB storage. SLPips/TPPips are pip distances; the DB columns
	// hold actual price levels so they don't overflow on high-priced symbols like BTCUSDT.
	mid := price.Mid
	if mid == 0 {
		mid = (price.Bid + price.Ask) / 2
	}
	var slPrice, tpPrice float64
	if result.Signal == "BUY" {
		slPrice = mid - result.SLPips*0.0001
		tpPrice = mid + result.TPPips*0.0001
	} else {
		slPrice = mid + result.SLPips*0.0001
		tpPrice = mid - result.TPPips*0.0001
	}
	sentAt := time.Now()

	orderID, err := b.orders.Insert(ctx, order.Order{
		SignalID: &signalID,
		Provider: b.provider.Name(),
		SymbolID: b.symbolUUID,
		Side:     result.Signal,
		Volume:   volume,
		SL:       &slPrice,
		TP:       &tpPrice,
		SentAt:   &sentAt,
	})
	if err != nil {
		slog.Error("insert order record failed", "err", err)
	}

	if _, err = b.provider.PlaceMarketOrder(ctx, result.Signal, volume, result.SLPips, result.TPPips); err != nil {
		slog.Error("order failed", "err", err)
		b.orders.UpdateError(ctx, orderID, "SEND_FAILED", err.Error())
		b.events.Insert(ctx, "error", map[string]any{
			"error": err.Error(), "stage": "place_order",
		}, ms(sentAt))
		return
	}

	b.pendingOrder = true
	b.pendingOrderID = orderID
	b.pendingOrderSentAt = sentAt
	b.pendingSide = result.Signal
	b.pendingTier = result.Tier
	b.pendingSLPrice = result.SLPrice
	b.pendingTPPrice = result.TPPrice
	b.pendingATR = result.ATR

	// Binance spot: no execution event will come, so register the position immediately.
	if b.provider.Name() == "binance" {
		mid := price.Mid
		if mid == 0 {
			mid = (price.Bid + price.Ask) / 2
		}
		b.registry.Register(trackedPosition{
			ProviderPositionID: orderID,
			Side:               result.Signal,
			Tier:               result.Tier,
			Volume:             volume,
			OpenPrice:          mid,
			SLPrice:            result.SLPrice,
			TPPrice:            result.TPPrice,
			ATR:                result.ATR,
			OpenTime:           sentAt,
		})
		b.pendingOrder = false
	}

	slog.Info("order sent",
		"signal", result.Signal,
		"tier", result.Tier,
		"confluence", result.Confluence,
		"volume", volume,
		"slPips", fmt.Sprintf("%.1f", result.SLPips),
		"tpPips", fmt.Sprintf("%.1f", result.TPPips),
	)
	b.events.Insert(ctx, "order_sent", map[string]any{
		"order_id":   orderID,
		"signal_id":  signalID,
		"side":       result.Signal,
		"tier":       result.Tier,
		"confluence": result.Confluence,
		"volume":     volume,
	}, ms(sentAt))
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
		Provider:           b.provider.Name(),
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

	b.registry.Register(trackedPosition{
		ProviderPositionID: provPosID,
		Side:               b.pendingSide,
		Tier:               b.pendingTier,
		Volume:             deal.FilledVolume,
		OpenPrice:          deal.ExecutionPrice,
		SLPrice:            b.pendingSLPrice,
		TPPrice:            b.pendingTPPrice,
		ATR:                b.pendingATR,
		OpenTime:           deal.ExecTime,
	})

	volume := deal.Volume
	filledVolume := deal.FilledVolume
	commission := deal.Commission
	if err := b.fills.Insert(ctx, fill.Fill{
		OurOrderID:         &b.pendingOrderID,
		Provider:           b.provider.Name(),
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

	slog.Info("position opened",
		"posID", provPosID, "side", b.pendingSide,
		"price", deal.ExecutionPrice, "tier", b.pendingTier,
	)
	b.events.Insert(ctx, "position_opened", map[string]any{
		"deal_id":     deal.DealID,
		"position_id": provPosID,
		"price":       deal.ExecutionPrice,
		"tier":        b.pendingTier,
	}, 0)
}

func (b *Bot) recordCloseFill(ctx context.Context, exec provider.ExecutionEvent) {
	if !exec.HasDeal || !exec.Deal.IsClose {
		return
	}
	deal := exec.Deal
	cl := deal.Close
	provOrderID := fmt.Sprintf("%d", deal.OrderID)
	provPosID := fmt.Sprintf("%d", deal.PositionID)

	// Grab everything we need before removing from registry.
	var maxFav, maxAdv *float64
	var closeReason *string
	if tracked, ok := b.registry.Get(provPosID); ok {
		maxFav = &tracked.MaxFavorable
		maxAdv = &tracked.MaxAdverse
	}
	if reason, ok := b.pendingCloseReasons[provPosID]; ok {
		closeReason = &reason
		delete(b.pendingCloseReasons, provPosID)
	} else {
		// Position was closed by cTrader's SL/TP mechanism — infer from P&L.
		r := inferCloseReason(cl.GrossProfit)
		closeReason = &r
	}
	b.registry.Remove(provPosID)

	if err := b.positions.Close(ctx, b.provider.Name(), provPosID, deal.ExecTime, maxFav, maxAdv); err != nil {
		slog.Error("positions.Close failed", "err", err)
	}

	closeSide := "SELL"
	if deal.TradeSide == 1 {
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
		Provider:           b.provider.Name(),
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
		CloseReason:        closeReason,
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

	slog.Info("position closed",
		"posID", provPosID,
		"grossProfit", cl.GrossProfit,
		"realized", realized,
	)
	b.events.Insert(ctx, "position_closed", map[string]any{
		"deal_id":      deal.DealID,
		"gross_profit": cl.GrossProfit,
		"balance":      cl.Balance,
	}, 0)
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

func (b *Bot) onTick(ctx context.Context, price provider.PriceEvent) {
	b.currentPrice = price
	t := tick.Tick{
		SymbolID:     b.symbolUUID,
		Bid:          price.Bid,
		Ask:          price.Ask,
		ReceivedAt:   price.Timestamp,
		ProcessingUS: time.Since(price.Timestamp).Microseconds(),
	}
	go func() {
		if err := b.ticks.Insert(context.Background(), t); err != nil {
			slog.Error("tick insert failed", "err", err)
		}
	}()
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
			slog.Info("credentials refreshed", "provider", b.provider.Name())
		}
	}
}

// weekendPositionCloser closes all open positions at 21:30 UTC on Friday, 30 minutes
// before the forex market closes at 22:00 UTC. Prevents positions being held over the
// weekend gap where SL/TP cannot execute.
func (b *Bot) weekendPositionCloser(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			utc := t.UTC()
			if utc.Weekday() != time.Friday {
				continue
			}
			if utc.Hour() != 21 || utc.Minute() != 30 {
				continue
			}
			if b.registry.Count() == 0 {
				continue
			}
			slog.Warn("Friday 21:30 UTC — closing all positions before market close")
			for _, pos := range b.registry.All() {
				b.closeTrackedPosition(ctx, pos, "weekend_close")
			}
			b.events.Insert(ctx, "weekend_close", map[string]any{
				"reason": "forex market closes at 22:00 UTC",
			}, 0)
		}
	}
}

// buildMarketStateSnapshots converts the current cached states for all timeframes
// into the compact snapshot stored in signals.checked_market_states.
func buildMarketStateSnapshots(states map[string]indicator.MarketState) map[string]signal.MarketStateSnapshot {
	out := make(map[string]signal.MarketStateSnapshot, len(states))
	for period, ms := range states {
		if !ms.IsWarmedUp {
			continue
		}
		out[period] = signal.MarketStateSnapshot{
			Regime:            ms.Regime,
			ADX:               ms.ADX,
			RSI:               ms.RSI,
			EMAFast:           ms.EMAFast,
			EMASlow:           ms.EMASlow,
			ATR:               ms.ATR,
			VolumeMA:          ms.VolumeMA,
			MomentumDirection: ms.MomentumDirection,
			SupportLevel:      ms.SupportLevel,
			ResistanceLevel:   ms.ResistanceLevel,
		}
	}
	return out
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


func (b *Bot) sendTestPosition(ctx context.Context) {
	if b.pendingOrder || b.registry.Count() > 0 {
		slog.Info("DEV: test position skipped — order already pending or position open",
			"provider", b.provider.Name())
		return
	}

	// cTrader: 100_000 = 1 lot (broker minimum). Binance: 100_000 satoshis = 0.001 BTC ≈ $60.
	testVolume := int64(100_000)
	const (
		testSLPips float64 = 10.0
		testTPPips float64 = 20.0
	)

	slog.Warn("DEV: sending test BUY position",
		"provider", b.provider.Name(),
		"symbol", b.symbol,
		"volume", testVolume,
		"slPips", testSLPips,
		"tpPips", testTPPips,
	)

	sentAt := time.Now()
	orderID, err := b.orders.Insert(ctx, order.Order{
		Provider: b.provider.Name(),
		SymbolID: b.symbolUUID,
		Side:     "BUY",
		Volume:   testVolume,
		SentAt:   &sentAt,
	})
	if err != nil {
		slog.Error("DEV: test order record insert failed", "err", err)
	}

	if _, err := b.provider.PlaceMarketOrder(ctx, "BUY", testVolume, testSLPips, testTPPips); err != nil {
		slog.Error("DEV: test position placement failed", "provider", b.provider.Name(), "err", err)
		b.orders.UpdateError(ctx, orderID, "SEND_FAILED", err.Error())
		return
	}

	b.pendingOrder = true
	b.pendingOrderID = orderID
	b.pendingOrderSentAt = sentAt
	b.pendingSide = "BUY"
	b.pendingTier = TierNormal
	b.pendingSLPrice = 0
	b.pendingTPPrice = 0
	b.pendingATR = 0

	// Binance spot has no execution event — register the position immediately.
	if b.provider.Name() == "binance" {
		mid := b.currentPrice.Mid
		if mid == 0 {
			mid = (b.currentPrice.Bid + b.currentPrice.Ask) / 2
		}
		b.registry.Register(trackedPosition{
			ProviderPositionID: orderID,
			Side:               "BUY",
			Tier:               TierNormal,
			Volume:             testVolume,
			OpenPrice:          mid,
			OpenTime:           sentAt,
		})
		b.pendingOrder = false
		slog.Info("DEV: test position registered (Binance)", "posID", orderID, "openPrice", mid)
	}
}

// inferCloseReason returns "tp_hit" or "sl_hit" based on whether cTrader's
// automatic order produced a gain or a loss. Used when the bot didn't initiate
// the close itself (i.e., no entry in pendingCloseReasons).
func inferCloseReason(grossProfit float64) string {
	if grossProfit >= 0 {
		return "tp_hit"
	}
	return "sl_hit"
}
