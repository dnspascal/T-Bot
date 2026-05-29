# Multi-Timeframe Subscription Complete ✅

## What Was Done

### 1. Database Schema Updated
**Files Modified:**
- `000003_create_market_states_table.up.sql` - Full market context storage
- `000008_create_signals.up.sql` - Lean signal records with multi-timeframe references

**Key Changes:**
- `market_states` now stores all indicators (EMA, RSI, ADX, ATR, support/resistance, regime, volatility/momentum trends)
- `signals` references market_states flexibly via JSONB (scales to any timeframe combo)
- Both tables ready for M5, H1, H4, D1+ timeframes

### 2. cTrader API Layer
**File:** `internal/api/constants.go`
- Added all period constants: M1, M2, M3, M4, M5, M10, M15, M30, H1, H4, D1, W1, MN1
- Added `PeriodToString(period uint32)` helper function

**File:** `internal/api/proto.go`
- Updated `Trendbar` struct to include `Period` field
- Modified `decodeTrendbar()` to populate Period from proto message

**File:** `internal/api/client.go`
- Changed `SubscribeLiveTrendbar()` signature: now accepts `period uint32` parameter
- Allows subscribing to any timeframe

### 3. Bot Implementation
**File:** `cmd/bot/main.go`
- Changed subscription from single M5 to loop subscribing to M5, H1, H4, D1
- Added logging: "subscribed to live trendbar" with period list

**File:** `internal/bot/bot.go`
- Updated `onTrendbar()` to handle different timeframes via `bar.Period`
- Only M5 triggers signal processing (for now)
- Added `storeCandle()` method to persist all timeframe candles with their period
- All 4 timeframes stored in `candles` table with correct `period` column

---

## How It Works Now

### Subscription Flow
```
cTrader sends:
  M5 candle → onTrendbar() → storeCandle(bar, "M5")
  H1 candle → onTrendbar() → storeCandle(bar, "H1")
  H4 candle → onTrendbar() → storeCandle(bar, "H4")
  D1 candle → onTrendbar() → storeCandle(bar, "D1")

All candles stored in:
  candles table (symbol_id, period, open, high, low, close, bar_time)
```

### Storage in Database
```
candles table:
symbol_id | period | open   | high   | low    | close  | bar_time
----------|--------|--------|--------|--------|--------|--------------------
EUR_UUID  | M5     | 1.1648 | 1.1655 | 1.1647 | 1.1651 | 2026-05-29 14:20:00
EUR_UUID  | H1     | 1.1640 | 1.1665 | 1.1638 | 1.1655 | 2026-05-29 14:00:00
EUR_UUID  | H4     | 1.1620 | 1.1670 | 1.1618 | 1.1650 | 2026-05-29 12:00:00
EUR_UUID  | D1     | 1.1500 | 1.1700 | 1.1490 | 1.1650 | 2026-05-29 00:00:00
```

---

## What's Next

### Phase 1: Indicator Calculation (Ready to Implement)
```
For EACH candle received (M5, H1, H4, D1):
  1. Calculate EMA(9), EMA(21)
  2. Calculate RSI(14)
  3. Calculate ADX(14), ATR(14)
  4. Detect support/resistance
  5. Classify regime
  6. Store in market_states table
```

### Phase 2: Market State Detection
```
On M5 close, load:
  - M5 market_state
  - H1 market_state (at that moment)
  - H4 market_state (at that moment)
  - D1 market_state (at that moment)

Calculate confluence:
  - How many timeframes agree on direction?
  - Store signal with references to all 4
```

### Phase 3: Multi-Timeframe Signal Generation
```
Only enter if:
  - M5 signal = BUY
  - H1 regime = trending_up (optional check)
  - H4 regime = trending_up (optional check)
  - D1 regime = trending_up (optional check)

Confluence score = how many agree (0-4)
Confidence score = backtest win rate for this confluence level
```

---

## Code Status
✅ Compiles successfully
✅ Ready to test subscription
⏳ Waiting for: Indicator calculation implementation

## Test When Ready
```bash
make build
# Run bot, verify logs show:
# "subscribed to live trendbar" periods: ["M5", "H1", "H4", "D1"]
# "store candle failed" errors (should be none if DB running)
# Verify candles table has rows with period in (M5, H1, H4, D1)
```
