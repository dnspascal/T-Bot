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
	"github.com/gorilla/websocket"
)

const (
	wsBaseURL    = "wss://fstream.binance.com/ws"
	wsTestnetURL = "wss://stream.binancefuture.com/ws"

	wsPingInterval = 3 * time.Minute 
	wsReadTimeout  = 5 * time.Minute 
)

type jsonFloat float64

func (f *jsonFloat) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		v, err := strconv.ParseFloat(string(b[1:len(b)-1]), 64)
		if err != nil {
			return err
		}
		*f = jsonFloat(v)
		return nil
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*f = jsonFloat(v)
	return nil
}

type wsEnvelope struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

type wsKlineEvent struct {
	Symbol string  `json:"s"`
	K      wsKline `json:"k"`
}

type wsKline struct {
	Symbol         string    `json:"s"`
	Interval       string    `json:"i"`
	OpenTime       int64     `json:"t"`
	CloseTime      int64     `json:"T"`
	Open           jsonFloat `json:"o"`
	High           jsonFloat `json:"h"`
	Low            jsonFloat `json:"l"`
	Close          jsonFloat `json:"c"`
	Volume         jsonFloat `json:"v"`
	QuoteVolume    jsonFloat `json:"Q"`
	TakerBuyVolume jsonFloat `json:"V"`
	TradeCount     int64     `json:"n"`
	IsClosed       bool      `json:"x"`
}

type wsBookTicker struct {
	Symbol  string    `json:"s"`
	Bid     jsonFloat `json:"b"`
	BidSize jsonFloat `json:"B"`
	Ask     jsonFloat `json:"a"`
	AskSize jsonFloat `json:"A"`
}

type wsTrade struct {
	Symbol    string    `json:"s"`
	TradeID   int64     `json:"t"`
	Price     jsonFloat `json:"p"`
	Qty       jsonFloat `json:"q"`
	BuyerID   int64     `json:"b"`
	SellerID  int64     `json:"a"`
	TradeTime int64     `json:"T"`
	IsBuyer   bool      `json:"m"`
}

type WebSocketClient struct {
	symbol  string
	period  string
	baseURL string
	conn    *websocket.Conn
	mu      sync.RWMutex

	priceCh  chan PriceData
	klineCh  chan KlineData
	tradeCh  chan TradeData
	closedCh chan struct{}

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
	Symbol         string
	Interval       string
	OpenTime       int64
	Open           float64
	High           float64
	Low            float64
	Close          float64
	Volume         float64
	QuoteVolume    float64
	TakerBuyVolume float64
	TradeCount     int64
	CloseTime      int64
}

type TradeData struct {
	Symbol    string
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

func (w *WebSocketClient) buildURL() string {
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
	return fmt.Sprintf("%s/stream?streams=%s@bookTicker/%s/%s@trade",
		baseURLWithoutWs, symbol, klineStreams.String(), symbol)
}

func (w *WebSocketClient) dial() error {
	wsURL := w.buildURL()
	slog.Info("connecting to websocket", "url", wsURL)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		return nil
	})
	conn.SetReadDeadline(time.Now().Add(wsReadTimeout))

	w.mu.Lock()
	if w.conn != nil {
		w.conn.Close()
	}
	w.conn = conn
	w.mu.Unlock()
	return nil
}

func (w *WebSocketClient) Connect() error {
	if err := w.dial(); err != nil {
		return err
	}
	slog.Info("websocket connected")
	go w.readLoop()
	go w.keepAlive()
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

func (w *WebSocketClient) PriceChan() <-chan PriceData { return w.priceCh }
func (w *WebSocketClient) KlineChan() <-chan KlineData { return w.klineCh }
func (w *WebSocketClient) TradeChan() <-chan TradeData { return w.tradeCh }
func (w *WebSocketClient) ClosedChan() <-chan struct{}  { return w.closedCh }

func (w *WebSocketClient) keepAlive() {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.mu.RLock()
			conn := w.conn
			w.mu.RUnlock()
			if conn == nil {
				continue
			}
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				slog.Debug("websocket keepalive ping failed", "err", err)
			}
		case <-w.ctx.Done():
			return
		}
	}
}

func (w *WebSocketClient) readLoop() {
	defer close(w.priceCh)
	defer close(w.klineCh)
	defer close(w.tradeCh)

	backoff := 2 * time.Second
	for {
		err := w.readMessages()
		if w.ctx.Err() != nil {
			slog.Info("websocket context cancelled")
			return
		}
		slog.Warn("websocket disconnected, reconnecting", "err", err, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-w.ctx.Done():
			return
		}
		if err := w.dial(); err != nil {
			slog.Error("websocket reconnect failed", "err", err)
			if backoff < 60*time.Second {
				backoff *= 2
			}
			continue
		}
		slog.Info("websocket reconnected")
		backoff = 2 * time.Second
	}
}

func (w *WebSocketClient) readMessages() error {
	for {
		select {
		case <-w.ctx.Done():
			return nil
		default:
		}

		w.mu.RLock()
		conn := w.conn
		w.mu.RUnlock()

		if conn == nil {
			return fmt.Errorf("connection is nil")
		}

		var raw json.RawMessage
		if err := conn.ReadJSON(&raw); err != nil {
			return err
		}
		conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		w.processMessage(raw)
	}
}

func (w *WebSocketClient) processMessage(data json.RawMessage) {
	var env wsEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		slog.Debug("failed to unmarshal envelope", "err", err)
		return
	}

	payload := env.Data
	if payload == nil {
		payload = data
	}

	switch {
	case strings.Contains(env.Stream, "@kline"):
		w.processKline(payload)
	case strings.Contains(env.Stream, "@trade"):
		w.processTrade(payload)
	case strings.Contains(env.Stream, "@bookTicker"):
		w.processBookTicker(payload)
	}
}

func (w *WebSocketClient) processBookTicker(data json.RawMessage) {
	var t wsBookTicker
	if err := json.Unmarshal(data, &t); err != nil {
		slog.Debug("failed to unmarshal bookTicker", "err", err)
		return
	}

	select {
	case w.priceCh <- PriceData{
		Bid:       float64(t.Bid),
		BidSize:   float64(t.BidSize),
		Ask:       float64(t.Ask),
		AskSize:   float64(t.AskSize),
		Timestamp: time.Now(),
	}:
	default:
		// bookTicker fires hundreds/sec; channel is intentionally throttled — silent drop is expected.
	}
}

func (w *WebSocketClient) processKline(data json.RawMessage) {
	var event wsKlineEvent
	if err := json.Unmarshal(data, &event); err != nil {
		slog.Debug("failed to unmarshal kline", "err", err)
		return
	}

	k := event.K
	if !k.IsClosed {
		return
	}

	select {
	case w.klineCh <- KlineData{
		Symbol:         k.Symbol,
		Interval:       k.Interval,
		OpenTime:       k.OpenTime,
		Open:           float64(k.Open),
		High:           float64(k.High),
		Low:            float64(k.Low),
		Close:          float64(k.Close),
		Volume:         float64(k.Volume),
		QuoteVolume:    float64(k.QuoteVolume),
		TakerBuyVolume: float64(k.TakerBuyVolume),
		TradeCount:     k.TradeCount,
		CloseTime:      k.CloseTime,
	}:
	default:
		slog.Warn("kline channel full, dropping message")
	}
}

func (w *WebSocketClient) processTrade(data json.RawMessage) {
	var t wsTrade
	if err := json.Unmarshal(data, &t); err != nil {
		slog.Debug("failed to unmarshal trade", "err", err)
		return
	}

	select {
	case w.tradeCh <- TradeData{
		Symbol:    t.Symbol,
		TradeID:   t.TradeID,
		Price:     float64(t.Price),
		Qty:       float64(t.Qty),
		BuyerID:   t.BuyerID,
		SellerID:  t.SellerID,
		TradeTime: t.TradeTime,
		IsBuyer:   t.IsBuyer,
	}:
	default:
	}
}
