# Phase 2 Implementation Plan: Dynamic Exit Logic

## Overview
Add market regime detection (ADX/ATR) + early exit signals to prevent position bleed.

---

## Step 1: ADX & ATR Calculation in Strategy
**File:** `internal/strategy/combined_strategy.go`

**What:** Add two new methods to `CombinedStrategy` struct
```go
// Calculate ADX(14) from price data
func (cs *CombinedStrategy) ADX(period int) float64

// Calculate ATR(14) from high/low/close
func (cs *CombinedStrategy) ATR(period int) float64
```

**Why:** Need these indicators to detect when trend is weakening (ADX) and volatility context (ATR)

**Data Used:** 
- Input: Historical candles already stored in `cs.candles` (close prices)
- Output: Returns float64 value, no DB writes yet

---

## Step 2: Exit Signal Detection in Strategy
**File:** `internal/strategy/combined_strategy.go`

**What:** Add new method that checks if open position should exit
```go
type ExitReason string
const (
    ExitHold        ExitReason = "HOLD"
    ExitEMABack     ExitReason = "ema_crossback"
    ExitDrawdown    ExitReason = "drawdown"
    ExitADXDecline  ExitReason = "adx_decline"
)

// Returns ExitReason if position should close, or ExitHold if should stay open
func (cs *CombinedStrategy) CheckExitSignals(
    currentCandle *Candle,
    openPosition *OpenPositionContext,  // peak price, peak profit, side, entry time
) ExitReason
```

**Logic:**
1. Check if EMA9 crossed back (reversed direction)
2. Check if ADX < 20 (trend weakening)
3. Check if drawdown from peak > 40% (bleeding profit)
4. If 2+ signals fire → return exit reason

**Data Used:**
- Input: Current candle + open position tracking data (in-memory)
- Output: ExitReason enum, no DB writes yet

---

## Step 3: Position State Tracking in Bot
**File:** `internal/bot/bot.go`

**What:** Track live position P&L in the Bot struct
```go
type Bot struct {
    // ... existing fields ...
    openPositionState *OpenPositionContext
}

type OpenPositionContext struct {
    PositionID       string           // our internal position ID
    Side             string           // BUY or SELL
    EntryPrice       float64
    EntryTime        time.Time
    PeakPrice        float64          // highest price reached since entry
    PeakProfit       float64          // highest $ profit reached
    CurrentProfit    float64          // live profit at current tick price
    DrawdownPercent  float64          // (PeakProfit - CurrentProfit) / PeakProfit * 100
}
```

**Where Updated:**
- On `onTradeFill()` (position opens) → initialize `openPositionState`
- On `onTick()` → recalculate `PeakPrice`, `CurrentProfit`, `DrawdownPercent` every tick
- On `onTradeFill()` (position closes) → clear `openPositionState`

**Data Used:**
- Input: Tick prices from cTrader, position data from `positions` table
- Output: All in-memory, no DB writes

---

## Step 4: Exit Logic in Bot.onCandle()
**File:** `internal/bot/bot.go`

**What:** Before entering NEW trade, check if OPEN position should exit
```go
func (b *Bot) onCandle(ctx context.Context, candle *Candle) {
    // NEW: Check exit signals on open position first
    if b.openPositionState != nil {
        exitReason := b.strat.CheckExitSignals(candle, b.openPositionState)
        if exitReason != strategy.ExitHold {
            // Close the position with this exit reason
            b.closeOpenPosition(ctx, exitReason)
            return  // Don't enter new trade in same candle
        }
    }

    // EXISTING: Check entry signals for new trades
    signal := b.strat.Signal(...)
    if signal == BUY { ... }
    // ...
}
```

**Data Used/Updated:**
- Reads from: `openPositionState` (in-memory), current candle
- Writes to: Calls `closeOpenPosition()` if needed

---

## Step 5: Close Position with Exit Reason
**File:** `internal/bot/bot.go`

**What:** New method to close position and record why it closed
```go
func (b *Bot) closeOpenPosition(ctx context.Context, exitReason ExitReason) error {
    // Send SELL/BUY order to cTrader to close position
    orderID := b.client.SendCloseOrder(...)
    
    // Log the exit event
    b.events.Insert(ctx, "position_exit", map[string]any{
        "position_id": b.openPositionState.PositionID,
        "exit_reason": exitReason,
        "peak_profit": b.openPositionState.PeakProfit,
        "current_profit": b.openPositionState.CurrentProfit,
    })
    
    // Clear the state
    b.openPositionState = nil
    
    return nil
}
```

**Data Used/Updated:**
- Reads from: `openPositionState`, `events` table (for logging)
- Writes to: `orders` table (close order), `events` table

---

## Step 6: Record Exit Reason in Fills Table
**File:** `internal/bot/bot.go` — in `onTradeFill()` method

**What:** When cTrader confirms position closure, set `fills.close_reason`
```go
func (b *Bot) onTradeFill(ctx context.Context, deal *Deal) {
    // ... existing code ...
    
    // When this fill CLOSES a position:
    if deal.ClosesPosition {
        closeReason := "manual"  // default
        if b.lastExitReason != "" {
            closeReason = b.lastExitReason  // "ema_crossback", "drawdown", etc.
        }
        
        b.fills.Insert(ctx, fill.Fill{
            // ... existing fields ...
            CloseReason: &closeReason,  // NEW
        })
    }
}
```

**Data Used/Updated:**
- Reads from: `b.lastExitReason` (set in `closeOpenPosition()`)
- Writes to: `fills` table, `close_reason` column

---

## Step 7: Optional - Store Market State
**File:** `internal/bot/bot.go` — in `onCandle()` method

**What:** After calculating ADX/ATR, optionally store them for analysis
```go
func (b *Bot) onCandle(ctx context.Context, candle *Candle) {
    adx := b.strat.ADX(14)
    atr := b.strat.ATR(14)
    
    // Optional: persist to database for analysis
    b.marketStates.Insert(ctx, market_state.MarketState{
        SymbolID: candle.SymbolID,
        Period:   "M5",
        ADX:      adx,
        ATR:      atr,
        Regime:   b.detectRegime(adx),  // "strong_trend", "weak_trend", "ranging"
        CandleID: candle.ID,
        RecordedAt: time.Now(),
    })
}
```

**Data Used/Updated:**
- Reads from: ADX/ATR calculations
- Writes to: `market_states` table (optional, only for analysis/logging)

---

## Implementation Order
1. **Step 1** → Build ADX/ATR calculators
2. **Step 2** → Build exit signal checker
3. **Step 3** → Add position state tracking to Bot
4. **Step 4** → Wire up exit logic in onCandle()
5. **Step 5** → Implement closeOpenPosition()
6. **Step 6** → Record close_reason in fills
7. **Step 7** → (Optional) Store market state

---

## Files to Modify
```
internal/strategy/combined_strategy.go    ← ADX, ATR, CheckExitSignals
internal/bot/bot.go                       ← Position tracking, exit logic, closeOpenPosition
internal/bot/position_context.go          ← NEW: OpenPositionContext struct (optional separate file)
```

## Tables Involved
```
market_states    ← Write ADX/ATR per candle (optional, Step 7)
fills            ← Update close_reason column when position closes (Step 6)
orders           ← Write when close order sent (Step 5, existing behavior)
events           ← Write position_exit event (Step 5, optional logging)
positions        ← Already written when position opens (existing)
```

## No Database Migrations Needed
All tables already exist with required columns.

---

## Success Criteria
✅ Bot detects when market reverses while holding position
✅ Position closes BEFORE hitting SL when signals align (e.g., EMA back + ADX weak)
✅ `fills.close_reason` shows why position closed (ema_crossback, drawdown, etc.)
✅ Peak profit vs current profit tracked and visible in logs
✅ `market_states` table shows ADX/ATR trend over time (if Step 7 done)

---

## Example Trade Lifecycle
```
14:20 → EMA9 crosses EMA21 UP + RSI > 50 → OPEN BUY position
        positionState = { EntryPrice: 1.1650, PeakPrice: 1.1650, PeakProfit: $1.80 }

14:21 → Price: 1.16520 → positionState.PeakPrice = 1.16520, PeakProfit = $1.85

14:22 → Price: 1.16470 → CurrentProfit = $1.20, DrawdownPercent = 35%

14:23 → Price: 1.16450, EMA9 crosses EMA21 DOWN + ADX < 20
        CheckExitSignals() returns "ema_crossback"
        closeOpenPosition() sends SELL to close
        
14:24 → cTrader fills SELL order
        onTradeFill() records: close_reason = "ema_crossback"
        
14:25 → Next candle, positionState = nil, ready for new entry
```
