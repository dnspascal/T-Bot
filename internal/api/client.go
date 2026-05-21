package api

import (
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type PriceEvent struct {
	Bid       float64
	Ask       float64
	Mid       float64
	Timestamp time.Time
}

type ExecutionEvent struct {
	Type      string // ORDER_FILLED | ORDER_REJECTED | ORDER_CANCELLED | ...
	Deal      DealInfo
	HasDeal   bool
	Timestamp time.Time
}

type CtidAccount struct {
	CtidTraderAccountID int64
	IsLive              bool
	TraderLogin         int64 
}

type Client struct {
	conn      *Connection
	accountID int64
	symbolID  int64

	mu      sync.Mutex
	authed  bool
	PriceCh     chan PriceEvent
	ExecutionCh chan ExecutionEvent
	TrendbarCh  chan Trendbar 

	traderResCh    chan TraderInfo
	reconcileResCh chan []OpenPosition
	trendbarsResCh chan []Trendbar
	accountListCh  chan []CtidAccount
}

func NewClient(demo bool, accountID, symbolID int64) *Client {
	c := &Client{
		accountID:      accountID,
		symbolID:       symbolID,
		PriceCh:        make(chan PriceEvent, 100),
		ExecutionCh:    make(chan ExecutionEvent, 10),
		TrendbarCh:     make(chan Trendbar, 10),
		traderResCh:    make(chan TraderInfo, 1),
		reconcileResCh: make(chan []OpenPosition, 1),
		trendbarsResCh: make(chan []Trendbar, 1),
		accountListCh:  make(chan []CtidAccount, 1),
	}
	c.conn = NewConnection(demo, c.handleMessage)
	return c
}

func (c *Client) Connect() error {
	return c.conn.Connect()
}

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) Dead() <-chan struct{} {
	return c.conn.dead
}

func (c *Client) GetAccountList(accessToken string) ([]CtidAccount, error) {
	if err := c.conn.SendRaw(ProtoOAGetAccountListByAccessTokenReq,
		encodeGetAccountListReq(accessToken)); err != nil {
		return nil, fmt.Errorf("GetAccountList send: %w", err)
	}
	select {
	case accounts := <-c.accountListCh:
		return accounts, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("GetAccountList: timeout waiting for response")
	}
}

func (c *Client) SetAccountID(id int64) {
	c.accountID = id
}

func (c *Client) AuthApp(clientID, clientSecret string) error {
	return c.conn.SendRaw(ProtoOAApplicationAuthReq,
		encodeAppAuthReq(clientID, clientSecret))
}

func (c *Client) AuthAccount(accessToken string) error {
	return c.conn.SendRaw(ProtoOAAccountAuthReq,
		encodeAccountAuthReq(c.accountID, accessToken))
}

func (c *Client) FetchAccountInfo() (TraderInfo, error) {
	if err := c.conn.SendRaw(ProtoOATraderReq, encodeTraderReq(c.accountID)); err != nil {
		return TraderInfo{}, fmt.Errorf("FetchAccountInfo send: %w", err)
	}
	select {
	case info := <-c.traderResCh:
		return info, nil
	case <-time.After(10 * time.Second):
		return TraderInfo{}, fmt.Errorf("FetchAccountInfo: timeout waiting for response")
	}
}

func (c *Client) Reconcile() ([]OpenPosition, error) {
	if err := c.conn.SendRaw(ProtoOAReconcileReq, encodeReconcileReq(c.accountID)); err != nil {
		return nil, fmt.Errorf("Reconcile send: %w", err)
	}
	select {
	case positions := <-c.reconcileResCh:
		return positions, nil
	case <-time.After(10 * time.Second):
		return nil, fmt.Errorf("Reconcile: timeout waiting for response")
	}
}
		
func (c *Client) SubscribeSpots() error {
	return c.conn.SendRaw(ProtoOASubscribeSpotsReq,
		encodeSubscribeSpotsReq(c.accountID, c.symbolID))
}


func (c *Client) SubscribeLiveTrendbar() error {
	return c.conn.SendRaw(ProtoOASubscribeLiveTrendbarReq,
		encodeSubscribeLiveTrendbarReq(c.accountID, c.symbolID, PeriodM5))
}


func (c *Client) FetchHistoricalTrendbars(count int) ([]Trendbar, error) {
	if err := c.conn.SendRaw(ProtoOAGetTrendbarsReq,
		encodeGetTrendbarsReq(c.accountID, c.symbolID, PeriodM5, time.Now().UnixMilli(), uint32(count))); err != nil {
		return nil, fmt.Errorf("FetchHistoricalTrendbars send: %w", err)
	}
	select {
	case bars := <-c.trendbarsResCh:
		return bars, nil
	case <-time.After(15 * time.Second):
		return nil, fmt.Errorf("FetchHistoricalTrendbars: timeout waiting for response")
	}
}


func (c *Client) PlaceMarketOrder(side uint32, volume int64, sl, tp float64) error {
	c.mu.Lock()
	authed := c.authed
	c.mu.Unlock()

	if !authed {
		return fmt.Errorf("not authenticated")
	}

	slog.Info("placing order",
		"side", sideStr(side),
		"volume", volume,
		"sl", sl,
		"tp", tp,
	)
	return c.conn.SendRaw(ProtoOANewOrderReq,
		encodeNewOrderReq(c.accountID, c.symbolID, side, volume, sl, tp))
}

func (c *Client) handleMessage(payloadType uint32, payload []byte) {
	switch payloadType {
	case ProtoOAApplicationAuthRes:
		slog.Info("app authenticated")

	case ProtoOAAccountAuthRes:
		c.mu.Lock()
		c.authed = true
		c.mu.Unlock()
		slog.Info("account authenticated", "accountID", c.accountID)

	case ProtoOASpotEvent:
		bid, ask, ok := decodeSpotEvent(payload)
		if ok {
			const divisor = 100000.0
			bidF := float64(bid) / divisor
			askF := float64(ask) / divisor
			event := PriceEvent{
				Bid:       bidF,
				Ask:       askF,
				Mid:       (bidF + askF) / 2,
				Timestamp: time.Now().UTC(),
			}
			select {
			case c.PriceCh <- event:
			default:
			}
		}
		
		if bar, ok := decodeLiveTrendbarEvent(payload); ok {
			slog.Debug("live trendbar received",
				"openTime", bar.OpenTime,
				"open", bar.Open,
				"high", bar.High,
				"low", bar.Low,
				"close", bar.Close,
			)
			select {
			case c.TrendbarCh <- bar:
			default:
			}
		}

	case ProtoOASubscribeLiveTrendbarRes:
		slog.Info("live trendbar subscription confirmed")

	case ProtoOAGetTrendbarsRes:
		bars := decodeGetTrendbarsRes(payload)
		slog.Info("historical trendbars received", "count", len(bars))
		select {
		case c.trendbarsResCh <- bars:
		default:
		}

	case ProtoOATraderRes:
		slog.Debug("ProtoOATraderRes received", "payloadHex", fmt.Sprintf("%x", payload))
		if info, ok := decodeTraderRes(payload); ok {
			slog.Info("account info received",
				"balance", info.Balance,
				"leverage", info.Leverage,
				"broker", info.BrokerName,
			)
			select {
			case c.traderResCh <- info:
			default:
			}
		} else {
			slog.Warn("decodeTraderRes failed — trader field not found in payload")
		}

	case ProtoOAReconcileRes:
		positions := decodeReconcileRes(payload)
		slog.Info("reconcile received", "openPositions", len(positions))
		select {
		case c.reconcileResCh <- positions:
		default:
		}

	case ProtoOAExecutionEvent:
		execType, deal, hasDeal := decodeFullExecutionEvent(payload)
		slog.Info("execution event received",
			"type", execType,
			"dealID", deal.DealID,
			"positionID", deal.PositionID,
			"executionPrice", deal.ExecutionPrice,
			"isClose", deal.IsClose,
		)
		select {
		case c.ExecutionCh <- ExecutionEvent{Type: execType, Deal: deal, HasDeal: hasDeal, Timestamp: time.Now().UTC()}:
		default:
		}

	case ProtoOAOrderErrorEvent:
		errorCode, orderID, description := decodeOrderError(payload)
		slog.Error("order error event",
			"errorCode", errorCode,
			"orderID", orderID,
			"description", description,
		)

	case ProtoOAGetAccountListByAccessTokenRes:
		accounts := decodeAccountListRes(payload)
		slog.Info("account list received", "count", len(accounts))
		select {
		case c.accountListCh <- accounts:
		default:
		}

	case ProtoOASubscribeSpotsRes:
		slog.Info("spot subscription confirmed")

	case ProtoOAErrorRes:
		code, desc := decodeOAError(payload)
		slog.Error("cTrader OA error", "code", code, "description", desc)

	case 50:
		code, desc := decodeGenericError(payload)
		slog.Error("cTrader protocol error", "code", code, "description", desc)

	case 51: 

	default:
		slog.Debug("unhandled message", "payloadType", payloadType)
	}
}

func sideStr(side uint32) string {
	if side == TradeSideBuy {
		return "BUY"
	}
	return "SELL"
}
