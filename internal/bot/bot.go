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

const pendingOrderTimeout = 30 * time.Second
const pendingCloseTimeout = 30 * time.Second

type pendingClose struct {
	reason string
	sentAt time.Time
}

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
	leverage  float64

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

	forceTestOrder bool 

	pendingCloseReasons map[string]pendingClose

	tickCh      chan tick.Tick 
	lastTickSaved time.Time    

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
	leverage float64,
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
		leverage:       leverage,
		registry:            newPositionRegistry(),
		pendingCloseReasons: make(map[string]pendingClose),
		tickCh:              make(chan tick.Tick, 500),
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
	go b.tickWriter(ctx)
	if b.provider.Name() == "ctrader" {
		go b.weekendPositionCloser(ctx)
	}

	priceCh := b.provider.PriceChan()
	candleCh := b.provider.CandleChan()
	execCh := b.provider.ExecutionChan()
	discCh := b.provider.DisconnectedChan()

	if b.cfg.SendTestPosition {
		b.forceTestOrder = true
		slog.Warn("SEND_TEST_POSITION=true — will place one real BUY on next M5 close via full pipeline")
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
	reconcileOK := false
	brokerPositions, err := b.provider.ReconcilePositions(ctx)
	if err != nil {
		slog.Warn("startup reconcile: could not fetch broker positions, trusting DB",
			"provider", b.provider.Name(), "err", err,
		)
	} else {
		reconcileOK = true
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

		if reconcileOK && !brokerOpen[p.ProviderPositionID] {
			slog.Warn("startup reconcile: position closed at broker while bot was offline — purging",
				"provider", b.provider.Name(), "posID", p.ProviderPositionID,
			)
			b.reconcileOfflineClose(ctx, p)
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
			Tier:               p.Tier,
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


type dealFetcher interface {
	FetchClosedDeal(positionID string, openTime time.Time) (*provider.DealInfo, error)
}


func (b *Bot) reconcileOfflineClose(ctx context.Context, p position.Position) {
	posID := p.ProviderPositionID
	var openTime time.Time
	if p.OpenTimestamp != nil {
		openTime = *p.OpenTimestamp
	}

	df, canFetch := b.provider.(dealFetcher)
	if !canFetch {
		slog.Warn("startup reconcile: provider does not support deal history — position marked closed without PnL",
			"posID", posID,
		)
		if err := b.positions.Close(ctx, b.provider.Name(), posID, time.Now(), nil, nil); err != nil {
			slog.Error("startup reconcile: positions.Close failed", "posID", posID, "err", err)
		}
		return
	}

	deal, err := df.FetchClosedDeal(posID, openTime)
	if err != nil {
		slog.Error("startup reconcile: FetchClosedDeal failed — marking closed without PnL",
			"posID", posID, "err", err,
		)
		if dbErr := b.positions.Close(ctx, b.provider.Name(), posID, time.Now(), nil, nil); dbErr != nil {
			slog.Error("startup reconcile: positions.Close failed", "posID", posID, "err", dbErr)
		}
		return
	}
	if deal == nil {
		slog.Warn("startup reconcile: close deal not found in broker history — marking closed without PnL",
			"posID", posID,
		)
		if err := b.positions.Close(ctx, b.provider.Name(), posID, time.Now(), nil, nil); err != nil {
			slog.Error("startup reconcile: positions.Close failed", "posID", posID, "err", err)
		}
		return
	}

	cl := deal.Close
	if cl == nil {
		slog.Warn("startup reconcile: deal has no closePositionDetail — marking closed without PnL",
			"posID", posID, "dealID", deal.DealID,
		)
		if err := b.positions.Close(ctx, b.provider.Name(), posID, deal.ExecTime, nil, nil); err != nil {
			slog.Error("startup reconcile: positions.Close failed", "posID", posID, "err", err)
		}
		return
	}

	closeTime := deal.ExecTime
	if closeTime.IsZero() {
		closeTime = time.Now()
	}
	if err := b.positions.Close(ctx, b.provider.Name(), posID, closeTime, nil, nil); err != nil {
		slog.Error("startup reconcile: positions.Close failed", "posID", posID, "err", err)
	}

	closeSide := "SELL"
	if deal.TradeSide == 1 { // TradeSideBuy (1): closing order was BUY → position was SELL
		closeSide = "BUY"
	}
	provPosID := posID
	reason := inferCloseReason(cl.GrossProfit)
	dealID := fmt.Sprintf("%d", deal.DealID)
	orderID := fmt.Sprintf("%d", deal.OrderID)
	entryPrice := cl.EntryPrice
	closedVolume := cl.ClosedVolume
	grossProfit := cl.GrossProfit
	swap := cl.Swap
	closeCommission := cl.Commission
	balanceAfter := cl.Balance
	pnlFee := cl.PnLConversionFee
	dealCommission := deal.Commission

	if err := b.fills.Insert(ctx, fill.Fill{
		Provider:           b.provider.Name(),
		ProviderFillID:     dealID,
		ProviderOrderID:    &orderID,
		ProviderPositionID: &provPosID,
		SymbolID:           b.symbolUUID,
		Side:               closeSide,
		Volume:             &deal.Volume,
		FilledVolume:       &deal.FilledVolume,
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
		CloseReason:        &reason,
		ProviderCreateTime: &deal.CreateTime,
		ProviderExecTime:   &deal.ExecTime,
		ReceivedAt:         time.Now(),
	}); err != nil {
		slog.Error("startup reconcile: fills.Insert failed", "posID", posID, "err", err)
	}

	realized := cl.GrossProfit + cl.Commission + cl.Swap
	isWin := realized > 0
	if err := b.pnls.Upsert(ctx, b.symbolUUID, realized, cl.GrossProfit, cl.Commission, cl.Swap, isWin, 0, 0); err != nil {
		slog.Error("startup reconcile: pnls.Upsert failed", "posID", posID, "err", err)
	}
	b.riskMgr.RecordTrade(realized)

	slog.Info("startup reconcile: offline close recorded from broker history",
		"posID", posID,
		"dealID", deal.DealID,
		"closePrice", deal.ExecutionPrice,
		"grossProfit", cl.GrossProfit,
		"realized", realized,
		"reason", reason,
	)
}

func (b *Bot) onCandleReceived(ctx context.Context, c provider.Candle) {
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	b.storeCandle(dbCtx, c)

	states, err := b.processorMgr.ProcessCandle(dbCtx, c.Timeframe, c.OpenTime, c.Open, c.High, c.Low, c.Close, c.Volume, c.ReceivedAt)
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
			b.checkPeakDrawback(ctx, c.Close) 
			b.logM1State(c.Close)             
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
	if b.pendingOrder && time.Since(b.pendingOrderSentAt) > pendingOrderTimeout {
		slog.Warn("pending order timed out — clearing to allow new signals",
			"orderID", b.pendingOrderID,
			"elapsed", time.Since(b.pendingOrderSentAt).Round(time.Second),
		)
		b.pendingOrder = false
	}

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

	b.logUnrealizedPnL(mid)

	b.watchPositions(ctx, m5)

	evalStart := time.Now()
	result := evaluateEntry(states, mid, b.cfg.LondonNYOnly)

	if b.forceTestOrder && result.Signal == "HOLD" {
		slog.Warn("FORCE_TEST_ORDER: overriding HOLD with BUY for pipeline test")
		result = EntryResult{
			Signal:     "BUY",
			Confluence: 1,
			Tier:       TierNormal,
			SLPrice:    mid - m5.ATR*slATRMult,
			TPPrice:    mid + m5.ATR*tpATRMult,
			SLPips:     m5.ATR * slATRMult / pipSize,
			TPPips:     m5.ATR * tpATRMult / pipSize,
			ATR:        m5.ATR,
		}
		b.forceTestOrder = false
	}

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



func (b *Bot) sameDirLosingPosition(side string) string {
	mid := b.currentPrice.Mid
	if mid == 0 {
		mid = (b.currentPrice.Bid + b.currentPrice.Ask) / 2
	}

	var newest trackedPosition
	found := false
	for _, pos := range b.registry.All() {
		if pos.Side != side || pos.OpenPrice == 0 {
			continue
		}
		if !found || pos.OpenTime.After(newest.OpenTime) {
			newest = pos
			found = true
		}
	}
	if !found {
		return ""
	}

	var lossInPrice float64
	if side == "BUY" {
		lossInPrice = newest.OpenPrice - mid
	} else {
		lossInPrice = mid - newest.OpenPrice
	}
	if lossInPrice <= 0 {
		return "" 
	}

	var slDist float64
	if side == "BUY" {
		slDist = newest.OpenPrice - newest.SLPrice
	} else {
		slDist = newest.SLPrice - newest.OpenPrice
	}
	if slDist <= 0 {
		slDist = 5 * pipSize
	}

	if lossInPrice > slDist*0.4 {
		return newest.ProviderPositionID
	}
	return ""
}

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
		if exec.Type == "ORDER_FILLED" && exec.ClosedPositionID != "" {
			b.recordBrokerClose(ctx, exec)
			return
		}
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

	if blockingPosID := b.sameDirLosingPosition(result.Signal); blockingPosID != "" {
		slog.Info("signal skipped — same-direction position already in loss, not adding to loser",
			"side", result.Signal,
			"blockingPosID", blockingPosID,
		)
		return
	}

	if !b.riskMgr.CanTrade(b.getBalance()) {
		slog.Warn("daily loss limit hit — signal skipped",
			"dailyLoss", fmt.Sprintf("$%.2f", b.riskMgr.DailyLoss()),
			"limitPct", fmt.Sprintf("%.0f%%", b.riskMgr.MaxDailyLossPct()),
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

	if b.provider.Name() == "binance" {
		mid := b.currentPrice.Mid
		if mid == 0 {
			mid = (b.currentPrice.Bid + b.currentPrice.Ask) / 2
		}
		if mid > 0 {
			lev := b.leverage
			if lev <= 0 {
				lev = 1
			}
			maxAffordable := int64((b.getBalance() * lev / mid) * 100_000_000 * 0.80)
			if volume > maxAffordable {
				volume = maxAffordable
			}
		}
		const binanceMinVolume = 100_000
		if volume < binanceMinVolume {
			minUSD := (binanceMinVolume / 100_000_000.0) * mid
			slog.Warn("binance: signal skipped — balance too low to meet minimum order size",
				"balance_usd", b.getBalance(), "min_order_usd", minUSD,
			)
			return
		}
	}

	slPrice := result.SLPrice
	tpPrice := result.TPPrice
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

	if b.provider.Name() == "binance" {
		mid := price.Mid
		if mid == 0 {
			mid = (price.Bid + price.Ask) / 2
		}
		b.registry.Register(trackedPosition{
			ProviderPositionID: result.Signal + ":" + orderID,
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
		Tier:               b.pendingTier,
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

	var maxFav, maxAdv *float64
	var closeReason *string
	if tracked, ok := b.registry.Get(provPosID); ok {
		maxFav = &tracked.MaxFavorable
		maxAdv = &tracked.MaxAdverse
	}
	if pc, ok := b.pendingCloseReasons[provPosID]; ok {
		closeReason = &pc.reason
		delete(b.pendingCloseReasons, provPosID)
	} else {
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

	b.riskMgr.RecordTrade(realized)

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


func (b *Bot) recordBrokerClose(ctx context.Context, exec provider.ExecutionEvent) {
	posID := exec.ClosedPositionID

	tracked, hasTracked := b.registry.Get(posID)
	b.registry.Remove(posID)

	var closeReason string
	if pc, ok := b.pendingCloseReasons[posID]; ok {
		closeReason = pc.reason
		delete(b.pendingCloseReasons, posID)
	}

	if err := b.positions.Close(ctx, b.provider.Name(), posID, exec.Timestamp, nil, nil); err != nil {
		slog.Error("recordBrokerClose: positions.Close failed", "posID", posID, "err", err)
	}

	mid := b.currentPrice.Mid
	if mid == 0 {
		mid = (b.currentPrice.Bid + b.currentPrice.Ask) / 2
	}

	var estimatedPnL float64
	closeSide := "SELL"
	if hasTracked {
		if tracked.Side == "BUY" {
			estimatedPnL = b.unrealizedUSD(mid-tracked.OpenPrice, tracked.Volume)
			closeSide = "SELL"
		} else {
			estimatedPnL = b.unrealizedUSD(tracked.OpenPrice-mid, tracked.Volume)
			closeSide = "BUY"
		}
		if closeReason == "" {
			closeReason = inferCloseReason(estimatedPnL)
		}
	}

	fillID := fmt.Sprintf("broker_%s_%d", posID, exec.Timestamp.UnixMilli())
	if err := b.fills.Insert(ctx, fill.Fill{
		Provider:           b.provider.Name(),
		ProviderFillID:     fillID,
		ProviderPositionID: &posID,
		SymbolID:           b.symbolUUID,
		Side:               closeSide,
		ExecutionPrice:     &mid,
		EventType:          "close",
		CloseReason:        &closeReason,
		GrossProfit:        &estimatedPnL,
		ReceivedAt:         exec.Timestamp,
	}); err != nil {
		slog.Error("recordBrokerClose: fills.Insert failed", "posID", posID, "err", err)
	}

	isWin := estimatedPnL > 0
	if err := b.pnls.Upsert(ctx, b.symbolUUID, estimatedPnL, estimatedPnL, 0, 0, isWin, 0, 0); err != nil {
		slog.Error("recordBrokerClose: pnls.Upsert failed", "posID", posID, "err", err)
	}

	b.riskMgr.RecordTrade(estimatedPnL)

	go b.refreshBalance()

	slog.Warn("broker closed position without deal — financials estimated from current price",
		"posID", posID,
		"closeSide", closeSide,
		"execPrice", mid,
		"estimatedPnL", fmt.Sprintf("%.2f", estimatedPnL),
		"closeReason", closeReason,
	)
	b.events.Insert(ctx, "broker_close_no_deal", map[string]any{
		"position_id":   posID,
		"estimated_pnl": estimatedPnL,
		"close_reason":  closeReason,
		"exec_price":    mid,
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

func (b *Bot) onTick(_ context.Context, price provider.PriceEvent) {
	b.currentPrice = price
	if time.Since(b.lastTickSaved) < time.Second {
		return
	}
	b.lastTickSaved = time.Now()
	t := tick.Tick{
		SymbolID:     b.symbolUUID,
		Bid:          price.Bid,
		Ask:          price.Ask,
		ReceivedAt:   price.Timestamp,
		ProcessingUS: time.Since(price.Timestamp).Microseconds(),
	}
	select {
	case b.tickCh <- t:
	default:
	}
}

func (b *Bot) tickWriter(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t := <-b.tickCh:
			dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := b.ticks.Insert(dbCtx, t)
			cancel()
			if err != nil {
				slog.Error("tick insert failed", "err", err)
			}
		}
	}
}

func (b *Bot) Reset() {
	b.pendingOrder = false
	b.pendingOrderID = ""
	b.pendingCloseReasons = make(map[string]pendingClose)
	b.lastCandleOpenTime = 0
	b.lastCandleClose = 0
	b.pendingSide = ""
	b.pendingTier = 0
	b.pendingSLPrice = 0
	b.pendingTPPrice = 0
	b.pendingATR = 0
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
	ticker := time.NewTicker(55 * time.Minute)
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

func buildMarketStateSnapshots(states map[string]indicator.MarketState) map[string]signal.MarketStateSnapshot {
	out := make(map[string]signal.MarketStateSnapshot, len(states))
	for period, ms := range states {
		if !ms.IsWarmedUp || ms.ID == "" {
			continue
		}
		out[period] = signal.MarketStateSnapshot{MarketStateID: ms.ID}
	}
	return out
}

func SaveCredential(ctx context.Context, db *pgxpool.Pool, key, value string) error {
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

func inferCloseReason(grossProfit float64) string {
	if grossProfit >= 0 {
		return "tp_hit"
	}
	return "sl_hit"
}
