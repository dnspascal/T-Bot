package ctrader

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/denismgaya/t-bot/internal/provider/ctrader/api"
	"github.com/denismgaya/t-bot/internal/bot"
	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/provider"
	"github.com/denismgaya/t-bot/internal/snapshot"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CTrader struct {
	cfg       *config.Config
	ctCfg     *config.CTraderConfig
	client    *api.Client
	db        *pgxpool.Pool
	events    EventsRepo
	snaps     SnapshotsRepo
}

type EventsRepo interface {
	Insert(context.Context, string, map[string]any, int64) error
}

type SnapshotsRepo interface {
	Insert(context.Context, snapshot.Snapshot) error
}

func New(cfg *config.Config, client *api.Client, db *pgxpool.Pool, events EventsRepo, snaps SnapshotsRepo) *CTrader {
	return &CTrader{
		cfg:     cfg,
		ctCfg:   cfg.CTrader,
		client:  client,
		db:      db,
		events:  events,
		snaps:   snaps,
	}
}

func (c *CTrader) Connect() error {
	return c.client.Connect()
}

func (c *CTrader) StartStreaming() error {
	return nil
}

func (c *CTrader) Close() error {
	c.client.Close()
	return nil
}

func (c *CTrader) Name() string {
	return "ctrader"
}

func (c *CTrader) Auth(ctx context.Context) (*provider.AuthResult, error) {
	if token, err := bot.LoadCredential(ctx, c.db, "ctrader_access_token"); err == nil && token != "" {
		c.ctCfg.AccessToken = token
		slog.Info("loaded cTrader access token from DB")
	}

	// Authenticate app
	authStart := time.Now()
	if err := c.client.AuthApp(c.ctCfg.ClientID, c.ctCfg.ClientSecret); err != nil {
		c.events.Insert(ctx, "auth_fail", map[string]any{"error": err.Error(), "stage": "app_auth"}, elapsed(authStart))
		return nil, fmt.Errorf("app auth: %w", err)
	}
	time.Sleep(2 * time.Second)

	accounts, err := c.client.GetAccountList(c.ctCfg.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("get account list: %w", err)
	}

	var ctidAccountID int64
	mode := "demo"
	if c.ctCfg.Demo {
		mode = "demo"
	} else {
		mode = "live"
	}
	for _, acc := range accounts {
		if acc.IsLive == !c.ctCfg.Demo {
			ctidAccountID = acc.CtidTraderAccountID
			slog.Info("found trading account",
				"ctidTraderAccountID", acc.CtidTraderAccountID,
				"traderLogin", acc.TraderLogin,
				"isLive", acc.IsLive,
			)
			break
		}
	}
	if ctidAccountID == 0 {
		return nil, fmt.Errorf("no %s account found in account list (got %d accounts)", mode, len(accounts))
	}
	c.client.SetAccountID(ctidAccountID)

	if err := c.client.AuthAccount(c.ctCfg.AccessToken); err != nil {
		c.events.Insert(ctx, "auth_fail", map[string]any{"error": err.Error(), "stage": "account_auth"}, elapsed(authStart))
		return nil, fmt.Errorf("account auth: %w", err)
	}
	c.events.Insert(ctx, "auth_ok", map[string]any{"account_id": c.ctCfg.AccountID}, elapsed(authStart))
	time.Sleep(2 * time.Second)

	fetchStart := time.Now()
	traderInfo, err := c.client.FetchAccountInfo()
	if err != nil || traderInfo.Balance == 0 {
		if err != nil {
			slog.Warn("FetchAccountInfo failed, using configured initial balance", "err", err, "balance", c.cfg.InitialBalance)
		} else {
			slog.Warn("FetchAccountInfo returned empty balance (demo server limitation), using configured initial balance", "balance", c.cfg.InitialBalance)
		}
		traderInfo = api.TraderInfo{Balance: c.cfg.InitialBalance}
	}

	balance := traderInfo.Balance
	leverage := traderInfo.Leverage
	brokerName := traderInfo.BrokerName
	trigger := "startup"
	c.snaps.Insert(ctx, snapshot.Snapshot{
		Provider:       "ctrader",
		ProviderAcctID: fmt.Sprintf("%d", c.ctCfg.AccountID),
		Balance:        balance,
		LeverageRatio:  &leverage,
		BrokerName:     &brokerName,
		Trigger:        &trigger,
		SnapshottedAt:  time.Now(),
	})

	c.events.Insert(ctx, "account_snapshot", map[string]any{
		"balance":  balance,
		"leverage": leverage,
		"broker":   brokerName,
	}, elapsed(fetchStart))

	// Reconcile open positions
	reconcileStart := time.Now()
	openPositions, err := c.client.Reconcile()
	if err != nil {
		return nil, fmt.Errorf("reconcile: %w", err)
	}

	hasOpenPosition := false
	for _, pos := range openPositions {
		if pos.SymbolID == c.ctCfg.SymbolID {
			hasOpenPosition = true
			break
		}
	}
	slog.Info("startup reconcile", "openPositions", len(openPositions), "hasOpenPosition", hasOpenPosition)
	c.events.Insert(ctx, "reconcile", map[string]any{
		"open_positions":    len(openPositions),
		"has_open_position": hasOpenPosition,
	}, elapsed(reconcileStart))

	return &provider.AuthResult{
		Balance:         balance,
		HasOpenPosition: hasOpenPosition,
		AccountID:       fmt.Sprintf("%d", ctidAccountID),
		Leverage:        leverage,
		BrokerName:      brokerName,
	}, nil
}

func elapsed(t time.Time) int64 {
	return time.Since(t).Milliseconds()
}

// === ORDER PLACEMENT ===

func (c *CTrader) PlaceMarketOrder(
	ctx context.Context,
	side string,
	volume int64,
	slPips float64,
	tpPips float64,
) (orderID string, err error) {
	sideUint32 := stringToSide(side)
	err = c.client.PlaceMarketOrder(sideUint32, volume, slPips, tpPips)
	// Note: cTrader doesn't return order ID; the order is placed asynchronously
	// The actual order ID comes through the execution event channel
	return "", err
}

func (c *CTrader) PlaceMarketOrderWithTimeout(
	ctx context.Context,
	side string,
	volume int64,
	slPips float64,
	tpPips float64,
	timeout time.Duration,
) (orderID string, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := c.PlaceMarketOrder(ctx, side, volume, slPips, tpPips)
		done <- err
	}()

	select {
	case err := <-done:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *CTrader) CancelOrder(ctx context.Context, orderID string) error {
	return fmt.Errorf("CancelOrder not yet implemented for cTrader")
}

// === ACCOUNT & POSITIONS ===

func (c *CTrader) FetchAccountInfo(ctx context.Context) (*provider.AccountInfo, error) {
	info, err := c.client.FetchAccountInfo()
	if err != nil {
		return nil, err
	}
	return &provider.AccountInfo{
		AccountID:        fmt.Sprintf("%d", c.ctCfg.AccountID),
		Balance:          info.Balance,
		Leverage:         info.Leverage,
		UsedMargin:       0,
		FreeMargin:       0,
		AvailableBalance: info.Balance,
		Currency:         "USD",
		BrokerName:       info.BrokerName,
		IsLive:           !c.ctCfg.Demo,
	}, nil
}

func (c *CTrader) QueryOpenPositions(ctx context.Context, symbol string) ([]provider.Position, error) {
	positions, err := c.client.Reconcile()
	if err != nil {
		return nil, err
	}

	var result []provider.Position
	for _, pos := range positions {
		if symbol != "" && pos.SymbolID != c.ctCfg.SymbolID {
			continue
		}
		posID := fmt.Sprintf("%d", pos.PositionID)
		side := "BUY"
		if pos.Side == api.TradeSideSell {
			side = "SELL"
		}
		result = append(result, provider.Position{
			PositionID: posID,
			Symbol:     symbol,
			Side:       side,
			Volume:     pos.Volume,
			OpenPrice:  0,
			OpenTime:   time.Now(),
			CurrentSL:  nil,
			CurrentTP:  nil,
			Swap:       0,
			Commission: 0,
			UsedMargin: 0,
		})
	}
	return result, nil
}

func (c *CTrader) ClosePosition(ctx context.Context, positionID string, volume int64) (string, error) {
	var posID int64
	if _, err := fmt.Sscanf(positionID, "%d", &posID); err != nil {
		return "", fmt.Errorf("invalid positionID %q: %w", positionID, err)
	}
	if err := c.client.ClosePosition(posID, volume); err != nil {
		return "", fmt.Errorf("ClosePosition: %w", err)
	}
	return "", nil // closeOrderID arrives via ExecutionEvent when the fill comes back
}

func (c *CTrader) ReconcilePositions(ctx context.Context) ([]provider.Position, error) {
	return c.QueryOpenPositions(ctx, "")
}

// === MARKET DATA & SUBSCRIPTIONS ===

func (c *CTrader) SubscribeCandles(ctx context.Context, symbol, timeframe string) error {
	period := stringToPeriod(timeframe)
	return c.client.SubscribeLiveTrendbar(period)
}

func (c *CTrader) UnsubscribeCandles(ctx context.Context, symbol, timeframe string) error {
	return fmt.Errorf("UnsubscribeCandles not yet implemented for cTrader")
}

func (c *CTrader) FetchHistoricalCandles(
	ctx context.Context,
	symbol string,
	timeframe string,
	count int,
) ([]provider.Candle, error) {
	period := stringToPeriod(timeframe)
	bars, err := c.client.FetchHistoricalTrendbars(period, count)
	if err != nil {
		return nil, err
	}

	var result []provider.Candle
	for _, bar := range bars {
		result = append(result, provider.Candle{
			Symbol:    symbol,
			Timeframe: timeframe,
			OpenTime:  bar.OpenTime,
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    bar.Volume,
		})
	}
	return result, nil
}

func (c *CTrader) FetchLatestTick(ctx context.Context, symbol string) (*provider.PriceEvent, error) {
	return nil, fmt.Errorf("FetchLatestTick not yet implemented for cTrader")
}

// === CREDENTIALS & REFRESH ===

func (c *CTrader) RefreshCredentials(ctx context.Context) error {
	newAccessToken, _, err := api.RefreshToken(c.ctCfg.ClientID, c.ctCfg.ClientSecret, c.ctCfg.RefreshToken)
	if err != nil {
		return err
	}
	c.ctCfg.AccessToken = newAccessToken
	slog.Info("cTrader credentials refreshed")
	return nil
}

func (c *CTrader) GetCredentialStatus(ctx context.Context) (*provider.CredentialStatus, error) {
	valid := c.ctCfg.AccessToken != ""
	return &provider.CredentialStatus{
		IsValid:     valid,
		ExpiresAt:   nil,
		RefreshedAt: time.Now(),
		Message:     "cTrader token status unknown (no expiration info)",
	}, nil
}

func (c *CTrader) ValidateCredentials(ctx context.Context) error {
	if c.ctCfg.ClientID == "" || c.ctCfg.ClientSecret == "" {
		return fmt.Errorf("cTrader credentials incomplete: missing ClientID or ClientSecret")
	}
	if c.ctCfg.AccessToken == "" {
		return fmt.Errorf("cTrader AccessToken not set")
	}
	return nil
}

// === EVENT STREAMS ===

func (c *CTrader) PriceChan() <-chan provider.PriceEvent {
	out := make(chan provider.PriceEvent, 100)
	go func() {
		for event := range c.client.PriceCh {
			out <- provider.PriceEvent{
				Bid:          event.Bid,
				Ask:          event.Ask,
				Mid:          event.Mid,
				Symbol:       "",
				ProviderName: c.Name(),
				Timestamp:    event.Timestamp,
			}
		}
		close(out)
	}()
	return out
}

func (c *CTrader) ExecutionChan() <-chan provider.ExecutionEvent {
	out := make(chan provider.ExecutionEvent, 100)
	go func() {
		for event := range c.client.ExecutionCh {
			execEvent := provider.ExecutionEvent{
				Type:         event.Type,
				OrderID:      fmt.Sprintf("%d", event.Deal.OrderID),
				ProviderName: c.Name(),
				Timestamp:    event.Timestamp,
				HasDeal:      event.HasDeal,
			}
			if event.HasDeal {
				deal := event.Deal
				execEvent.Deal = &provider.DealInfo{
					DealID:         deal.DealID,
					OrderID:        deal.OrderID,
					PositionID:     deal.PositionID,
					FilledVolume:   deal.FilledVolume,
					Volume:         deal.Volume,
					ExecutionPrice: deal.ExecutionPrice,
					Commission:     deal.Commission,
					TradeSide:      deal.TradeSide,
					CreateTime:     deal.CreateTime,
					ExecTime:       deal.ExecTime,
					IsClose:        deal.IsClose,
				}
				if deal.IsClose {
					execEvent.Deal.Close = &provider.CloseInfo{
						ClosedVolume:     deal.Close.ClosedVolume,
						EntryPrice:       deal.Close.EntryPrice,
						GrossProfit:      deal.Close.GrossProfit,
						Swap:             deal.Close.Swap,
						Commission:       deal.Close.Commission,
						Balance:          deal.Close.Balance,
						PnLConversionFee: deal.Close.PnLConversionFee,
					}
				}
			}
			out <- execEvent
		}
		close(out)
	}()
	return out
}

func (c *CTrader) OrderChan() <-chan provider.OrderEvent {
	out := make(chan provider.OrderEvent, 100)
	go func() {
		for event := range c.client.ExecutionCh {
			orderEvent := provider.OrderEvent{
				OrderID:      fmt.Sprintf("%d", event.Deal.OrderID),
				Side:         sideToString(event.Deal.TradeSide),
				ProviderName: c.Name(),
				Timestamp:    event.Timestamp,
				Volume:       event.Deal.Volume,
			}
			price := event.Deal.ExecutionPrice
			orderEvent.ExecutionPrice = &price
			out <- orderEvent
		}
		close(out)
	}()
	return out
}

func (c *CTrader) CandleChan() <-chan provider.Candle {
	out := make(chan provider.Candle, 100)
	go func() {
		type pendingBar struct {
			openTime int64
			candle   provider.Candle
		}
		pending := make(map[string]pendingBar)

		for bar := range c.client.TrendbarCh {
			period := api.PeriodToString(bar.Period)
			if period == "UNKNOWN" {
				slog.Warn("ctrader: trendbar with unknown period", "periodCode", bar.Period)
				continue
			}
			current := provider.Candle{
				Timeframe:  period,
				OpenTime:   bar.OpenTime,
				Open:       bar.Open,
				High:       bar.High,
				Low:        bar.Low,
				Close:      bar.Close,
				Volume:     bar.Volume,
				ReceivedAt: time.Now(),
			}

			if prev, ok := pending[period]; ok && prev.openTime != bar.OpenTime {
				out <- prev.candle
			}
			pending[period] = pendingBar{openTime: bar.OpenTime, candle: current}
		}
		close(out)
	}()
	return out
}

func (c *CTrader) DisconnectedChan() <-chan struct{} {
	return c.client.Dead()
}

// === HELPER FUNCTIONS ===

func stringToSide(side string) uint32 {
	if side == "BUY" {
		return api.TradeSideBuy
	}
	return api.TradeSideSell
}

func sideToString(side uint32) string {
	if side == api.TradeSideBuy {
		return "BUY"
	}
	return "SELL"
}

func stringToPeriod(tf string) uint32 {
	switch tf {
	case "M1":
		return api.PeriodM1
	case "M2":
		return api.PeriodM2
	case "M3":
		return api.PeriodM3
	case "M4":
		return api.PeriodM4
	case "M5":
		return api.PeriodM5
	case "M10":
		return api.PeriodM10
	case "M15":
		return api.PeriodM15
	case "M30":
		return api.PeriodM30
	case "H1":
		return api.PeriodH1
	case "H4":
		return api.PeriodH4
	case "D1":
		return api.PeriodD1
	case "W1":
		return api.PeriodW1
	case "MN1":
		return api.PeriodMN1
	default:
		return api.PeriodM5
	}
}
