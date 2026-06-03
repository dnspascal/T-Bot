package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/util"
	"github.com/gorilla/websocket"
)

const (
	wsBaseURL = "wss://stream.binance.com:9443/ws"
	wsTestnetURL = "wss://stream.testnet.binance.vision/ws"
)

type WebSocketClient struct {
	symbol        string
	period        string  
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
	Symbol     string
	Interval   string
	OpenTime   int64
	Open       float64
	High       float64
	Low        float64
	Close      float64
	Volume     float64
	TradeCount int64
	CloseTime  int64
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

func NewWebSocketClient(symbol, period string, testnet bool) *WebSocketClient {
	baseURL := wsBaseURL
	if testnet {
		baseURL = wsTestnetURL
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WebSocketClient{
		symbol:   symbol,
		period:   period,
		baseURL:  baseURL,
		priceCh:  make(chan PriceData, 100),
		klineCh:  make(chan KlineData, 100),
		tradeCh:  make(chan TradeData, 100),
		closedCh: make(chan struct{}),
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (w *WebSocketClient) Connect() error {
	// Binance WebSocket requires lowercase symbols
	symbol := strings.ToLower(w.symbol)

	intervals := config.BinanceIntervals()
	var klineStreams strings.Builder
	for i, interval := range intervals {
		if i > 0 {
			klineStreams.WriteString("/")
		}
		fmt.Fprintf(&klineStreams, "%s@kline_%s", symbol, interval)
	}

	baseURLWithoutWs := strings.TrimSuffix(w.baseURL, "/ws")
	wsURL := fmt.Sprintf("%s/stream?streams=%s@bookTicker/%s/%s@trade",
		baseURLWithoutWs,
		symbol,
		klineStreams.String(),
		symbol,
	)

	slog.Info("connecting to websocket", "url", wsURL)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}

	slog.Info("websocket connected successfully")

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	go w.readLoop()
	return nil
}

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

	slog.Info("websocket read loop started")

	for {
		select {
		case <-w.ctx.Done():
			slog.Info("websocket context cancelled, exiting read loop")
			return
		default:
		}

		w.mu.RLock()
		conn := w.conn
		w.mu.RUnlock()

		if conn == nil {
			slog.Warn("websocket connection is nil, exiting read loop")
			return
		}

		var msg json.RawMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			slog.Error("websocket read error", "err", err)
			return
		}

		w.processMessage(msg)
	}
}

func (w *WebSocketClient) processMessage(data []byte) {
	var wrapper map[string]any
	if err := json.Unmarshal(data, &wrapper); err != nil {
		slog.Debug("failed to unmarshal message", "err", err)
		return
	}

	stream, _ := toString(wrapper["stream"])

	var msg map[string]any
	var ok bool
	if rawData, hasData := wrapper["data"]; hasData {
		msg, ok = rawData.(map[string]any)
		if !ok {
			return
		}
	} else {
		msg = wrapper
	}

	if strings.Contains(stream, "@kline") {
		w.processKline(msg)
	} else if strings.Contains(stream, "@trade") {
		w.processTrade(msg)
	} else if strings.Contains(stream, "@bookTicker") {
		w.processBookTicker(msg)
	}
}

func (w *WebSocketClient) processBookTicker(msg map[string]interface{}) {
	bid, bidOk := toFloat64(msg["b"])
	bids, bidsOk := toFloat64(msg["B"])
	ask, askOk := toFloat64(msg["a"])
	asks, asksOk := toFloat64(msg["A"])

	if !bidOk || !bidsOk || !askOk || !asksOk {
		slog.Debug("bookTicker missing fields", "bidOk", bidOk, "bidsOk", bidsOk, "askOk", askOk, "asksOk", asksOk)
		return
	}

	slog.Info("price tick", "bid", bid, "bidSize", bids, "ask", ask, "askSize", asks)

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
		slog.Warn("price channel full, dropping message")
	}
}

func (w *WebSocketClient) processKline(msg map[string]any) {
	k, ok := msg["k"].(map[string]any)
	if !ok {
		slog.Debug("kline missing 'k' field")
		return
	}

	isClosed, _ := k["x"].(bool)
	if !isClosed {
		return
	}

	util.WriteJSONLog("binance_kline.json", msg)

	symbol, _ := toString(msg["s"])
	interval, _ := toString(k["i"])
	openTime, openTimeOk := toInt64(k["t"])
	open, openOk := toFloat64(k["o"])
	high, highOk := toFloat64(k["h"])
	low, lowOk := toFloat64(k["l"])
	close, closeOk := toFloat64(k["c"])
	volume, volumeOk := toFloat64(k["v"])
	tradeCount, tradeCountOk := toInt64(k["n"])
	closeTime, closeTimeOk := toInt64(k["T"])

	if !openTimeOk || !openOk || !highOk || !lowOk || !closeOk || !volumeOk || !tradeCountOk || !closeTimeOk {
		slog.Warn("kline parse failed",
			"symbol", symbol, "interval", interval,
			"openTimeOk", openTimeOk, "openOk", openOk, "highOk", highOk,
			"lowOk", lowOk, "closeOk", closeOk, "volumeOk", volumeOk, "tradeCountOk", tradeCountOk, "closeTimeOk", closeTimeOk)
		return
	}


	kline := KlineData{
		Symbol:     symbol,
		Interval:   interval,
		OpenTime:   openTime,
		Open:       open,
		High:       high,
		Low:        low,
		Close:      close,
		Volume:     volume,
		TradeCount: tradeCount,
		CloseTime:  closeTime,
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

func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	}
	return 0, false
}

func toInt64(v any) (int64, bool) {
	switch val := v.(type) {
	case float64:
		return int64(val), true
	case string:
		i, err := strconv.ParseInt(val, 10, 64)
		return i, err == nil
	}
	return 0, false
}

func toString(v any) (string, bool) {
	str, ok := v.(string)
	return str, ok
}

func toBool(v any) (bool, bool) {
	b, ok := v.(bool)
	return b, ok
}
