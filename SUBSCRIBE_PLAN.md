# Subscribe to All Timeframes (M5, H1, H4, D1)

## Current State
- Only subscribes to M5 trendbar from cTrader
- `client.SubscribeLiveTrendbar()` in main.go

## Goal
- Subscribe to M5, H1, H4, D1 simultaneously
- Handle incoming candles from each timeframe
- Store them all in `candles` table with their `period` column

---

## Changes Needed

### 1. cTrader API Layer
**File:** `internal/api/client.go`

Currently:
```go
func (c *Client) SubscribeLiveTrendbar() error
```

Need to add:
```go
func (c *Client) SubscribeLiveTrendbar(period string) error
// period = "M5" or "H1" or "H4" or "D1"
```

OR change to:
```go
func (c *Client) SubscribeLiveTrendbar(periods []string) error
// periods = ["M5", "H1", "H4", "D1"]
```

### 2. Message Handling
**File:** `internal/api/client.go` and `internal/bot/bot.go`

When message arrives from cTrader, determine which timeframe and route accordingly:
```go
func (c *Client) onTrendbar(bar *ProtoOATrendbar) {
    period := determinePeriod(bar.Period)  // M5, H1, H4, D1
    
    switch period {
    case "M5":
        b.onCandleM5(bar)
    case "H1":
        b.onCandleH1(bar)
    case "H4":
        b.onCandleH4(bar)
    case "D1":
        b.onCandleD1(bar)
    }
}
```

OR simpler:
```go
func (c *Client) onTrendbar(bar *ProtoOATrendbar) {
    period := determinePeriod(bar.Period)
    b.onCandle(period, bar)  // unified handler
}
```

### 3. Bot Handler
**File:** `internal/bot/bot.go`

Need to handle candles from different timeframes:
```go
func (b *Bot) onCandle(ctx context.Context, period string, bar *api.Trendbar) {
    // Store candle
    b.candles.Insert(ctx, candle.Candle{
        SymbolID: b.cfg.SymbolUUID,
        Period:   period,  // M5, H1, H4, D1
        Open:     bar.Open,
        High:     bar.High,
        Low:      bar.Low,
        Close:    bar.Close,
        Volume:   bar.Volume,
        BarTime:  time.Unix(bar.OpenTime, 0).UTC(),
        ReceivedAt: time.Now(),
    })
    
    // Calculate market state for this timeframe
    marketState := b.strat.CalculateMarketState(period)
    b.marketStates.Insert(ctx, marketState)
    
    // Only generate signals on M5 close
    if period == "M5" {
        signal := b.strat.Signal(marketState)
        b.onSignal(ctx, signal)
    }
}
```

### 4. Subscription in main.go
**File:** `cmd/bot/main.go`

Change from:
```go
if err := client.SubscribeLiveTrendbar(); err != nil {
    log.Fatal("subscribe live trendbar:", err)
}
```

To:
```go
for _, period := range []string{"M5", "H1", "H4", "D1"} {
    if err := client.SubscribeLiveTrendbar(period); err != nil {
        log.Fatal("subscribe live trendbar:", err)
    }
}
```

---

## Implementation Steps

1. **Check cTrader API docs** — how to subscribe to different timeframes?
   - Is it separate subscription per period?
   - Or one subscription with period parameter?
   - What are the period codes/values?

2. **Update `client.SubscribeLiveTrendbar()`** — accept period parameter

3. **Update message handler** — route by period

4. **Update `bot.onCandle()`** — accept period, store with period

5. **Update main.go** — subscribe to all 4 periods

6. **Test startup** — verify all 4 periods coming in

---

## Success Criteria
✅ Bot receives M5, H1, H4, D1 candles from cTrader
✅ All stored in candles table with correct period
✅ Logs show candles arriving from all timeframes
✅ No errors or crashes

---

## After This
Then we can:
1. Calculate indicators per timeframe
2. Store market_states for each
3. Generate multi-timeframe signals
