package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsBaseURL = "wss://stream.binance.com:9443/ws"
	wsTestnetURL = "wss://stream.testnet.binance.vision/ws"
)

// WebSocketClient manages Binance WebSocket connections for market data
type WebSocketClient struct {
	symbol        string
	baseURL       string
	conn          *websocket.Conn
	mu            sync.RWMutex

	// Channels for market data
	priceCh   chan PriceData
	klineCh   chan KlineData
	tradeCh   chan TradeData
	closedCh  chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
}

type PriceData struct {
	Bid       float64
	Ask       float64
	BidSize   float64
	AskSize   float64
	Timestamp time.Time
}

type KlineData struct {
	Symbol    string
	Interval  string
	OpenTime  int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime int64
}

type TradeData struct {
	Symbol    string
	OrderID   int64
	TradeID   int64
	Price     float64
	Qty       float64
	BuyerID   int64
	SellerID  int64
	TradeTime int64
	IsBuyer   bool
}

// NewWebSocketClient creates a new WebSocket client
func NewWebSocketClient(symbol string, testnet bool) *WebSocketClient {
	baseURL := wsBaseURL
	if testnet {
		baseURL = wsTestnetURL
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WebSocketClient{
		symbol:   symbol,
		baseURL:  baseURL,
		priceCh:  make(chan PriceData, 100),
		klineCh:  make(chan KlineData, 100),
		tradeCh:  make(chan TradeData, 100),
		closedCh: make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Connect establishes WebSocket connections for price, klines, and trades
func (w *WebSocketClient) Connect() error {
	symbol := w.symbol
	wsURL := fmt.Sprintf("%s/%s@bookTicker/%s@kline_1m/%s@trade",
		w.baseURL,
		symbol,
		symbol,
		symbol,
	)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	// Start message reader loop
	go w.readLoop()

	slog.Info("binance websocket connected", "symbol", symbol)
	return nil
}

// Close closes the WebSocket connection
func (w *WebSocketClient) Close() error {
	w.cancel()
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.conn != nil {
		close(w.closedCh)
		return w.conn.Close()
	}
	return nil
}

// PriceChan returns the price data channel
func (w *WebSocketClient) PriceChan() <-chan PriceData {
	return w.priceCh
}

// KlineChan returns the kline data channel
func (w *WebSocketClient) KlineChan() <-chan KlineData {
	return w.klineCh
}

// TradeChan returns the trade data channel
func (w *WebSocketClient) TradeChan() <-chan TradeData {
	return w.tradeCh
}

// ClosedChan returns a channel that closes when the connection is closed
func (w *WebSocketClient) ClosedChan() <-chan struct{} {
	return w.closedCh
}

func (w *WebSocketClient) readLoop() {
	defer close(w.priceCh)
	defer close(w.klineCh)
	defer close(w.tradeCh)

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		w.mu.RLock()
		conn := w.conn
		w.mu.RUnlock()

		if conn == nil {
			return
		}

		var msg json.RawMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Info("websocket closed")
				return
			}
			slog.Error("websocket read error", "err", err)
			time.Sleep(time.Second)
			continue
		}

		w.processMessage(msg)
	}
}

func (w *WebSocketClient) processMessage(data []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		slog.Debug("failed to unmarshal message", "err", err)
		return
	}

	eventType, ok := msg["e"].(string)
	if !ok {
		return
	}

	switch eventType {
	case "bookTicker":
		w.processBookTicker(msg)
	case "kline":
		w.processKline(msg)
	case "trade":
		w.processTrade(msg)
	}
}

func (w *WebSocketClient) processBookTicker(msg map[string]interface{}) {
	bid, bidOk := toFloat64(msg["b"])
	bids, bidsOk := toFloat64(msg["B"])
	ask, askOk := toFloat64(msg["a"])
	asks, asksOk := toFloat64(msg["A"])

	if !bidOk || !bidsOk || !askOk || !asksOk {
		return
	}

	price := PriceData{
		Bid:       bid,
		BidSize:   bids,
		Ask:       ask,
		AskSize:   asks,
		Timestamp: time.Now(),
	}

	select {
	case w.priceCh <- price:
	default:
		// Channel full, drop message
	}
}

func (w *WebSocketClient) processKline(msg map[string]interface{}) {
	k, ok := msg["k"].(map[string]interface{})
	if !ok {
		return
	}

	symbol, _ := toString(msg["s"])
	interval, _ := toString(k["i"])
	openTime, _ := toInt64(k["t"])
	open, _ := toFloat64(k["o"])
	high, _ := toFloat64(k["h"])
	low, _ := toFloat64(k["l"])
	close, _ := toFloat64(k["c"])
	volume, _ := toFloat64(k["v"])
	closeTime, _ := toInt64(k["T"])

	kline := KlineData{
		Symbol:    symbol,
		Interval:  interval,
		OpenTime:  openTime,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
		CloseTime: closeTime,
	}

	select {
	case w.klineCh <- kline:
	default:
		// Channel full, drop message
	}
}

func (w *WebSocketClient) processTrade(msg map[string]interface{}) {
	symbol, _ := toString(msg["s"])
	orderID, _ := toInt64(msg["a"])
	tradeID, _ := toInt64(msg["t"])
	price, _ := toFloat64(msg["p"])
	qty, _ := toFloat64(msg["q"])
	buyerID, _ := toInt64(msg["b"])
	sellerID, _ := toInt64(msg["a"])
	tradeTime, _ := toInt64(msg["T"])
	isBuyer, _ := toBool(msg["m"])

	trade := TradeData{
		Symbol:    symbol,
		OrderID:   orderID,
		TradeID:   tradeID,
		Price:     price,
		Qty:       qty,
		BuyerID:   buyerID,
		SellerID:  sellerID,
		TradeTime: tradeTime,
		IsBuyer:   isBuyer,
	}

	select {
	case w.tradeCh <- trade:
	default:
		// Channel full, drop message
	}
}

// Helper functions for type conversions
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case string:
		var f float64
		fmt.Sscanf(val, "%f", &f)
		return f, true
	}
	return 0, false
}

func toInt64(v interface{}) (int64, bool) {
	switch val := v.(type) {
	case float64:
		return int64(val), true
	case string:
		var i int64
		fmt.Sscanf(val, "%d", &i)
		return i, true
	}
	return 0, false
}

func toString(v interface{}) (string, bool) {
	str, ok := v.(string)
	return str, ok
}

func toBool(v interface{}) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}
