package binance

import (
	"context"
	"encoding/json"
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

const (
	binanceBrokerName    = "Binance"
	binanceAccountMode   = "netted"
	binanceLeverage      = 1.0
	binanceQuoteCurrency = "USDT"
)

type Binance struct {
	cfg    *config.Config
	db     *pgxpool.Pool
	events EventsRepo
	snaps  SnapshotsRepo

	restClient *RestClient
	wsClient   *WebSocketClient

	// Event channels
	priceCh        chan provider.PriceEvent
	executionCh    chan provider.ExecutionEvent
	orderCh        chan provider.OrderEvent
	candleCh       chan provider.Candle
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
	slog.Info("binance provider created")
	return &Binance{
		cfg:            cfg,
		db:             db,
		events:         events,
		snaps:          snaps,
		priceCh:        make(chan provider.PriceEvent, 100),
		executionCh:    make(chan provider.ExecutionEvent, 10),
		orderCh:        make(chan provider.OrderEvent, 10),
		candleCh:       make(chan provider.Candle, 10),
		disconnectedCh: make(chan struct{}),
	}
}

func (b *Binance) Connect() error {
	slog.Info("binance provider connecting")

	if b.cfg.Binance == nil || b.cfg.Binance.APIKey == "" {
		return fmt.Errorf("Binance API key not configured")
	}

	b.restClient = NewRestClient(b.cfg.Binance.APIKey, b.cfg.Binance.APISecret, b.cfg.Binance.TestNet)

	valid, err := b.restClient.ValidateAPIKey()
	if err != nil {
		return fmt.Errorf("API key validation failed: %w", err)
	}
	if !valid {
		return fmt.Errorf("API key invalid or insufficient permissions")
	}

	b.wsClient = NewWebSocketClient(b.cfg.BinanceSymbol, b.cfg.Period, b.cfg.Binance.TestNet)
	if err := b.wsClient.Connect(); err != nil {
		return fmt.Errorf("websocket connect: %w", err)
	}

	go b.forwardPriceEvents()
	go b.forwardKlineEvents()

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

	account, err := b.restClient.GetAccount(false)
	if err != nil {
		b.events.Insert(ctx, "auth_fail", map[string]any{"error": err.Error()}, elapsed(authStart))
		return nil, fmt.Errorf("get account: %w", err)
	}

	var balance float64
	for _, bal := range account.Balances {
		if bal.Asset == binanceQuoteCurrency {
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

	openOrders, err := b.restClient.GetOpenOrders("")
	hasOpenPosition := len(openOrders) > 0

	b.events.Insert(ctx, "auth_ok", map[string]any{
		"balance":        balance,
		"open_positions": len(openOrders),
	}, elapsed(authStart))

	trigger := "startup"
	accountJSON, _ := json.Marshal(account)
	accountID := b.cfg.Binance.APIKey[:8]
	currency := binanceQuoteCurrency
	brokerName := binanceBrokerName
	accountMode := binanceAccountMode
	leverage := binanceLeverage

	b.snaps.Insert(ctx, snapshot.Snapshot{
		Provider:        "binance",
		ProviderAcctID:  accountID,
		Balance:         balance,
		Currency:        &currency,
		BrokerName:      &brokerName,
		AccountMode:     &accountMode,
		LeverageRatio:   &leverage,
		ProviderPayload: accountJSON,
		Trigger:         &trigger,
		SnapshottedAt:   time.Now(),
	})

	return &provider.AuthResult{
		Balance:         balance,
		HasOpenPosition: hasOpenPosition,
		AccountID:       accountID,
		Leverage:        binanceLeverage,
		BrokerName:      binanceBrokerName,
	}, nil
}

func (b *Binance) Setup() error {
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

	slog.Info("placing market order", "symbol", b.cfg.BinanceSymbol, "side", side, "qty", qty)
	orderID, err = b.restClient.PlaceMarketOrder(b.cfg.BinanceSymbol, side, qty)
	if err != nil {
		slog.Error("PlaceMarketOrder failed", "symbol", b.cfg.BinanceSymbol, "err", err)
		return "", err
	}

	slog.Info("market order placed", "orderID", orderID, "side", side, "volume", volume, "symbol", b.cfg.BinanceSymbol)
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
		if bal.Asset == binanceQuoteCurrency {
			val, _ := strconv.ParseFloat(bal.Free, 64)
			balance += val
			break
		}
	}

	return &provider.AccountInfo{
		AccountID:        b.cfg.Binance.APIKey[:8],
		Balance:          balance,
		Leverage:         binanceLeverage,
		UsedMargin:       0,
		FreeMargin:       balance,
		AvailableBalance: balance,
		Currency:         binanceQuoteCurrency,
		BrokerName:       binanceBrokerName,
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

	klines, err := b.restClient.GetKlines(symbol, interval, count)
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
	if b.cfg.Binance.APIKey == "" || b.cfg.Binance.APISecret == "" {
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
		slog.Debug("forwarding binance price", "bid", price.Bid, "ask", price.Ask)
		select {
		case b.priceCh <- provider.PriceEvent{
			Bid:          price.Bid,
			Ask:          price.Ask,
			Mid:          (price.Bid + price.Ask) / 2,
			Symbol:       b.cfg.BinanceSymbol,
			ProviderName: "binance",
			Timestamp:    price.Timestamp,
		}:
		default:
			slog.Warn("binance price channel full, dropping message")
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
	if interval, exists := config.PeriodToBinanceInterval[tf]; exists {
		return interval
	}
	return "1m"  // Fallback
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
