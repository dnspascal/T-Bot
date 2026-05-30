package binance

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/provider"
	"github.com/denismgaya/t-bot/internal/snapshot"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Binance struct {
	cfg    *config.Config
	db     *pgxpool.Pool
	events EventsRepo
	snaps  SnapshotsRepo

	restClient *RestClient
	wsClient   *WebSocketClient

	// Event channels
	priceCh      chan provider.PriceEvent
	executionCh  chan provider.ExecutionEvent
	orderCh      chan provider.OrderEvent
	candleCh     chan provider.Candle
	disconnectedCh chan struct{}

	mu sync.RWMutex
}

type EventsRepo interface {
	Insert(context.Context, string, map[string]any, int64) error
}

type SnapshotsRepo interface {
	Insert(context.Context, snapshot.Snapshot) error
}

func New(cfg *config.Config, db *pgxpool.Pool, events EventsRepo, snaps SnapshotsRepo) *Binance {
	return &Binance{
		cfg:      cfg,
		db:       db,
		events:   events,
		snaps:    snaps,
		priceCh: make(chan provider.PriceEvent, 100),
		executionCh: make(chan provider.ExecutionEvent, 10),
		orderCh: make(chan provider.OrderEvent, 10),
		candleCh: make(chan provider.Candle, 10),
		disconnectedCh: make(chan struct{}),
	}
}

func (b *Binance) Connect() error {
	if b.cfg.Binance == nil || b.cfg.BinanceAPIKey == "" {
		return fmt.Errorf("Binance API key not configured")
	}

	// Create REST client
	b.restClient = NewRestClient(b.cfg.BinanceAPIKey, b.cfg.BinanceAPISecret, b.cfg.Binance.TestNet)

	// Validate API key
	valid, err := b.restClient.ValidateAPIKey()
	if err != nil {
		return fmt.Errorf("API key validation failed: %w", err)
	}
	if !valid {
		return fmt.Errorf("API key invalid or insufficient permissions")
	}

	// Create WebSocket client
	b.wsClient = NewWebSocketClient("EURUSD", b.cfg.Binance.TestNet)
	if err := b.wsClient.Connect(); err != nil {
		return fmt.Errorf("websocket connect: %w", err)
	}

	// Start event forwarding goroutines
	go b.forwardPriceEvents()
	go b.forwardKlineEvents()

	slog.Info("Binance connected", "testnet", b.cfg.Binance.TestNet)
	return nil
}

func (b *Binance) Close() error {
	if b.wsClient != nil {
		b.wsClient.Close()
	}
	close(b.disconnectedCh)
	return nil
}

func (b *Binance) Name() string {
	return "binance"
}

func (b *Binance) Auth(ctx context.Context) (*provider.AuthResult, error) {
	if b.restClient == nil {
		return nil, fmt.Errorf("not connected - call Connect() first")
	}

	authStart := time.Now()

	// Fetch account info
	account, err := b.restClient.GetAccount(false)
	if err != nil {
		b.events.Insert(ctx, "auth_fail", map[string]any{"error": err.Error()}, elapsed(authStart))
		return nil, fmt.Errorf("get account: %w", err)
	}

	// Calculate total balance in USDT
	var balance float64
	for _, bal := range account.Balances {
		if bal.Asset == "USDT" {
			val, _ := strconv.ParseFloat(bal.Free, 64)
			balance += val
			val2, _ := strconv.ParseFloat(bal.Locked, 64)
			balance += val2
			break
		}
	}

	if balance == 0 {
		balance = b.cfg.InitialBalance
	}

	// Check for open orders
	openOrders, err := b.restClient.GetOpenOrders("")
	hasOpenPosition := len(openOrders) > 0

	b.events.Insert(ctx, "auth_ok", map[string]any{
		"balance":        balance,
		"open_positions": len(openOrders),
	}, elapsed(authStart))

	// Store snapshot
	trigger := "startup"
	b.snaps.Insert(ctx, snapshot.Snapshot{
		Provider:       "binance",
		ProviderAcctID: b.cfg.BinanceAPIKey[:8] + "...",
		Balance:        balance,
		Trigger:        &trigger,
		SnapshottedAt:  time.Now(),
	})

	return &provider.AuthResult{
		Balance:         balance,
		HasOpenPosition: hasOpenPosition,
		AccountID:       b.cfg.BinanceAPIKey[:8],
		Leverage:        1.0,
		BrokerName:      "Binance",
	}, nil
}

func (b *Binance) Setup() error {
	slog.Info("Binance setup complete")
	return nil
}

// === ORDER PLACEMENT ===

func (b *Binance) PlaceMarketOrder(
	ctx context.Context,
	side string,
	volume int64,
	slPips float64,
	tpPips float64,
) (orderID string, err error) {
	if b.restClient == nil {
		return "", fmt.Errorf("not connected")
	}

	// Convert volume from satoshis to decimal
	qty := float64(volume) / 100000000

	orderID, err = b.restClient.PlaceMarketOrder("EURUSD", side, qty)
	if err != nil {
		slog.Error("PlaceMarketOrder failed", "err", err)
		return "", err
	}

	slog.Info("market order placed", "orderID", orderID, "side", side, "volume", volume)
	return orderID, nil
}

func (b *Binance) PlaceMarketOrderWithTimeout(
	ctx context.Context,
	side string,
	volume int64,
	slPips float64,
	tpPips float64,
	timeout time.Duration,
) (orderID string, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan string, 1)
	go func() {
		id, _ := b.PlaceMarketOrder(ctx, side, volume, slPips, tpPips)
		done <- id
	}()

	select {
	case orderID := <-done:
		return orderID, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (b *Binance) CancelOrder(ctx context.Context, orderID string) error {
	return fmt.Errorf("CancelOrder not yet implemented for Binance")
}

// === ACCOUNT & POSITIONS ===

func (b *Binance) FetchAccountInfo(ctx context.Context) (*provider.AccountInfo, error) {
	if b.restClient == nil {
		return nil, fmt.Errorf("not connected")
	}

	account, err := b.restClient.GetAccount(false)
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}

	var balance float64
	for _, bal := range account.Balances {
		if bal.Asset == "USDT" {
			val, _ := strconv.ParseFloat(bal.Free, 64)
			balance += val
			break
		}
	}

	return &provider.AccountInfo{
		AccountID:        b.cfg.BinanceAPIKey[:8],
		Balance:          balance,
		Leverage:         1.0,
		UsedMargin:       0,
		FreeMargin:       balance,
		AvailableBalance: balance,
		Currency:         "USDT",
		BrokerName:       "Binance",
		IsLive:           !b.cfg.Binance.TestNet,
	}, nil
}

func (b *Binance) QueryOpenPositions(ctx context.Context, symbol string) ([]provider.Position, error) {
	if b.restClient == nil {
		return nil, fmt.Errorf("not connected")
	}

	orders, err := b.restClient.GetOpenOrders(symbol)
	if err != nil {
		return nil, err
	}

	var positions []provider.Position
	for _, order := range orders {
		qty, _ := strconv.ParseInt(order.ExecutedQty, 10, 64)
		price, _ := strconv.ParseFloat(order.Price, 64)

		positions = append(positions, provider.Position{
			PositionID: fmt.Sprintf("%d", order.OrderID),
			Symbol:     order.Symbol,
			Side:       order.Side,
			Volume:     qty,
			OpenPrice:  price,
			OpenTime:   time.Unix(0, order.Time*int64(time.Millisecond)),
		})
	}

	return positions, nil
}

func (b *Binance) ClosePosition(ctx context.Context, positionID string, volume int64) (closeOrderID string, err error) {
	return "", fmt.Errorf("ClosePosition not yet implemented for Binance")
}

func (b *Binance) ReconcilePositions(ctx context.Context) ([]provider.Position, error) {
	return b.QueryOpenPositions(ctx, "")
}

// === MARKET DATA & SUBSCRIPTIONS ===

func (b *Binance) SubscribeCandles(ctx context.Context, symbol, timeframe string) error {
	if b.wsClient == nil {
		return fmt.Errorf("websocket not connected")
	}
	slog.Info("Binance candle subscription", "symbol", symbol, "timeframe", timeframe)
	return nil
}

func (b *Binance) UnsubscribeCandles(ctx context.Context, symbol, timeframe string) error {
	return fmt.Errorf("UnsubscribeCandles not yet implemented")
}

func (b *Binance) FetchHistoricalCandles(
	ctx context.Context,
	symbol string,
	timeframe string,
	count int,
) ([]provider.Candle, error) {
	if b.restClient == nil {
		return nil, fmt.Errorf("not connected")
	}

	// Convert timeframe to Binance interval format
	interval := timeframeToInterval(timeframe)

	klines, err := b.restClient.GetKlines("EURUSD", interval, count)
	if err != nil {
		return nil, fmt.Errorf("get klines: %w", err)
	}

	var candles []provider.Candle
	for _, kline := range klines {
		open, _ := strconv.ParseFloat(kline.Open, 64)
		high, _ := strconv.ParseFloat(kline.High, 64)
		low, _ := strconv.ParseFloat(kline.Low, 64)
		close, _ := strconv.ParseFloat(kline.Close, 64)
		volume, _ := strconv.ParseInt(kline.Volume, 10, 64)

		candles = append(candles, provider.Candle{
			Symbol:    symbol,
			Timeframe: timeframe,
			OpenTime:  kline.OpenTime / 1000,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
		})
	}

	return candles, nil
}

func (b *Binance) FetchLatestTick(ctx context.Context, symbol string) (*provider.PriceEvent, error) {
	// Use last price from websocket if available
	// For now, return error
	return nil, fmt.Errorf("FetchLatestTick not yet implemented")
}

// === CREDENTIALS & REFRESH ===

func (b *Binance) RefreshCredentials(ctx context.Context) error {
	if b.restClient == nil {
		return fmt.Errorf("not connected")
	}

	valid, err := b.restClient.ValidateAPIKey()
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("API key invalid")
	}

	slog.Info("Binance credentials validated")
	return nil
}

func (b *Binance) GetCredentialStatus(ctx context.Context) (*provider.CredentialStatus, error) {
	valid := b.restClient != nil
	return &provider.CredentialStatus{
		IsValid:     valid,
		ExpiresAt:   nil,
		RefreshedAt: time.Now(),
		Message:     "Binance API keys do not expire",
	}, nil
}

func (b *Binance) ValidateCredentials(ctx context.Context) error {
	if b.cfg.BinanceAPIKey == "" || b.cfg.BinanceAPISecret == "" {
		return fmt.Errorf("Binance credentials incomplete")
	}
	return nil
}

// === EVENT STREAMS ===

func (b *Binance) PriceChan() <-chan provider.PriceEvent {
	return b.priceCh
}

func (b *Binance) ExecutionChan() <-chan provider.ExecutionEvent {
	return b.executionCh
}

func (b *Binance) OrderChan() <-chan provider.OrderEvent {
	return b.orderCh
}

func (b *Binance) CandleChan() <-chan provider.Candle {
	return b.candleCh
}

func (b *Binance) DisconnectedChan() <-chan struct{} {
	return b.disconnectedCh
}

// === PRIVATE METHODS ===

func (b *Binance) forwardPriceEvents() {
	for price := range b.wsClient.PriceChan() {
		select {
		case b.priceCh <- provider.PriceEvent{
			Bid:          price.Bid,
			Ask:          price.Ask,
			Mid:          (price.Bid + price.Ask) / 2,
			Symbol:       "EURUSD",
			ProviderName: "binance",
			Timestamp:    price.Timestamp,
		}:
		default:
		}
	}
}

func (b *Binance) forwardKlineEvents() {
	for kline := range b.wsClient.KlineChan() {
		select {
		case b.candleCh <- provider.Candle{
			Symbol:    kline.Symbol,
			Timeframe: intervalToTimeframe(kline.Interval),
			OpenTime:  kline.OpenTime / 1000,
			Open:      kline.Open,
			High:      kline.High,
			Low:       kline.Low,
			Close:     kline.Close,
			Volume:    int64(kline.Volume),
		}:
		default:
		}
	}
}

func elapsed(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}

func timeframeToInterval(tf string) string {
	switch tf {
	case "M1":
		return "1m"
	case "M5":
		return "5m"
	case "M15":
		return "15m"
	case "M30":
		return "30m"
	case "H1":
		return "1h"
	case "H4":
		return "4h"
	case "D1":
		return "1d"
	default:
		return "1m"
	}
}

func intervalToTimeframe(interval string) string {
	switch interval {
	case "1m":
		return "M1"
	case "5m":
		return "M5"
	case "15m":
		return "M15"
	case "30m":
		return "M30"
	case "1h":
		return "H1"
	case "4h":
		return "H4"
	case "1d":
		return "D1"
	default:
		return "M1"
	}
}
