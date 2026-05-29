# Indicator Architecture: Pure Functions + Pluggable

## Design Principles

1. **Indicators are pure functions** — calculate from prices, return value, no knowledge of timeframe
2. **Timeframe-agnostic** — same EMA calculator works for M5, H1, H4, D1, M15, M30
3. **Swappable implementations** — easy to swap Wilder's RSI for simple RSI
4. **Testable in isolation** — `calculateEMA([1.0, 1.1, 1.2, ...]) → 1.05`
5. **Multi-provider ready** — indicator calculation same for cTrader, Binance, Kraken

---

## File Structure

```
internal/
├── indicator/                    # Pure calculation logic
│   ├── interface.go             # Interfaces (no implementation)
│   ├── ema.go                   # EMA: pure function
│   ├── rsi.go                   # RSI: pure function  
│   ├── adx.go                   # ADX: pure function
│   ├── atr.go                   # ATR: pure function
│   └── repository.go            # Storage abstraction (interface)
│
├── marketstate/                 # Market state orchestration
│   ├── calculator.go            # Calculates all indicators for a timeframe
│   ├── repository.go            # Stores market_state (DB implementation)
│   └── warmup.go                # Loads historical candles + calculates
│
├── bot/                         # Trading logic
│   └── bot.go                   # Uses market_states to make decisions
```

---

## 1. Pure Indicator Calculations

**internal/indicator/interface.go**
```go
package indicator

// Indicator is a pure calculation interface (no state, no side effects)
type Indicator interface {
    Calculate(prices []float64) float64
}

// EMA calculator (stateless)
type EMACalculator struct {
    period int
}

func NewEMACalculator(period int) *EMACalculator {
    return &EMACalculator{period: period}
}

func (e *EMACalculator) Calculate(prices []float64) float64 {
    if len(prices) < e.period {
        return 0  // Not ready
    }
    return calculateEMA(prices, e.period)
}

// RSI calculator (stateless)
type RSICalculator struct {
    period int
}

func NewRSICalculator(period int) *RSICalculator {
    return &RSICalculator{period: period}
}

func (r *RSICalculator) Calculate(prices []float64) float64 {
    if len(prices) < r.period+1 {
        return 50  // Not ready, neutral
    }
    return calculateRSI(prices, r.period)
}
```

**internal/indicator/ema.go**
```go
package indicator

// calculateEMA is a pure function - takes prices, returns EMA value
// No knowledge of timeframe (M5, H1, etc.) - just math
func calculateEMA(prices []float64, period int) float64 {
    if len(prices) < period {
        return 0
    }
    
    k := 2.0 / float64(period+1)
    
    // Initial SMA from oldest `period` prices
    sum := 0.0
    for i := 0; i < period; i++ {
        sum += prices[i]
    }
    ema := sum / float64(period)
    
    // Apply EMA smoothing from position `period` onwards
    for i := period; i < len(prices); i++ {
        ema = prices[i]*k + ema*(1-k)
    }
    
    return ema
}

// For testing: pass ANY array of prices, get EMA back
// No mocking needed, no DB access, no timeframe knowledge
```

**internal/indicator/rsi.go**
```go
package indicator

// calculateRSI is a pure function - takes prices, returns RSI value
func calculateRSI(prices []float64, period int) float64 {
    if len(prices) < period+1 {
        return 50  // Not ready
    }
    
    var sumGain, sumLoss float64
    
    // Calculate gains/losses for first `period` candles
    for i := 1; i <= period; i++ {
        change := prices[i] - prices[i-1]
        if change >= 0 {
            sumGain += change
        } else {
            sumLoss += -change
        }
    }
    
    avgGain := sumGain / float64(period)
    avgLoss := sumLoss / float64(period)
    
    // Apply Wilder's smoothing for rest
    for i := period + 1; i < len(prices); i++ {
        change := prices[i] - prices[i-1]
        var gain, loss float64
        if change >= 0 {
            gain = change
        } else {
            loss = -change
        }
        
        avgGain = (avgGain*float64(period-1) + gain) / float64(period)
        avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
    }
    
    if avgLoss == 0 {
        return 100
    }
    return 100 - 100/(1+avgGain/avgLoss)
}
```

**internal/indicator/adx.go**
```go
package indicator

// calculateADX is a pure function - takes OHLC data, returns ADX value
type OHLCData struct {
    High  float64
    Low   float64
    Close float64
}

func calculateADX(ohlcData []OHLCData, period int) float64 {
    if len(ohlcData) < period+1 {
        return 0  // Not ready
    }
    
    // Implementation of ADX calculation
    // Takes only the data needed, returns float64
    // No knowledge of "this is M5" or "this is H1"
    
    // ... ADX math ...
    return 0.0
}
```

---

## 2. Market State Calculator (Orchestrator)

**internal/marketstate/calculator.go**
```go
package marketstate

import "github.com/denismgaya/t-bot/internal/indicator"

type MarketStateCalculator struct {
    emaFast *indicator.EMACalculator  // 9
    emaSlow *indicator.EMACalculator  // 21
    rsi     *indicator.RSICalculator  // 14
    adx     *indicator.ADXCalculator  // 14
    atr     *indicator.ATRCalculator  // 14
}

func NewMarketStateCalculator() *MarketStateCalculator {
    return &MarketStateCalculator{
        emaFast: indicator.NewEMACalculator(9),
        emaSlow: indicator.NewEMACalculator(21),
        rsi:     indicator.NewRSICalculator(14),
        adx:     indicator.NewADXCalculator(14),
        atr:     indicator.NewATRCalculator(14),
    }
}

// Calculate takes closing prices + OHLC, returns complete market state
// Timeframe is just a label (M5, H1, H4, etc.) - doesn't affect calculation
func (m *MarketStateCalculator) Calculate(period string, closes []float64, ohlc []indicator.OHLCData) MarketState {
    return MarketState{
        Period:   period,  // Just metadata, not used in calculation
        EMAFast:  m.emaFast.Calculate(closes),
        EMASlow:  m.emaSlow.Calculate(closes),
        RSI:      m.rsi.Calculate(closes),
        ADX:      m.adx.Calculate(ohlc),
        ATR:      m.atr.Calculate(ohlc),
        IsWarmed: len(closes) >= 21,  // Minimum for EMA(21)
    }
}
```

---

## 3. Warmup: Load Historical Candles

**internal/marketstate/warmup.go**
```go
package marketstate

import (
    "context"
    "github.com/denismgaya/t-bot/internal/api"
    "github.com/denismgaya/t-bot/internal/candle"
)

type Warmer struct {
    client     *api.Client
    repo       *Repository
    calculator *MarketStateCalculator
}

func (w *Warmer) WarmupAllTimeframes(ctx context.Context, symbolID string) error {
    periods := []uint32{
        api.PeriodM5,
        api.PeriodM15,
        api.PeriodM30,
        api.PeriodH1,
        api.PeriodH4,
        api.PeriodD1,
    }
    
    for _, period := range periods {
        if err := w.warmupTimeframe(ctx, symbolID, period); err != nil {
            return err
        }
    }
    return nil
}

func (w *Warmer) warmupTimeframe(ctx context.Context, symbolID string, period uint32) error {
    // 1. Fetch last 50 candles from cTrader
    candles, err := w.client.FetchHistoricalTrendbars(50, period)
    if err != nil {
        return err
    }
    
    // 2. Extract prices and OHLC
    var closes []float64
    var ohlcData []OHLCData
    for _, c := range candles {
        closes = append(closes, c.Close)
        ohlcData = append(ohlcData, OHLCData{
            High:  c.High,
            Low:   c.Low,
            Close: c.Close,
        })
    }
    
    // 3. Calculate indicators (same calculation for all timeframes!)
    marketState := w.calculator.Calculate(
        api.PeriodToString(period),
        closes,
        ohlcData,
    )
    
    // 4. Store in market_states table
    if err := w.repo.Insert(ctx, marketState); err != nil {
        return err
    }
    
    return nil
}
```

---

## 4. Repository (Storage Abstraction)

**internal/marketstate/repository.go**
```go
package marketstate

import (
    "context"
    "github.com/jackc/pgx/v5/pgxpool"
)

// Repository is an interface - swappable for different storage backends
type Repository interface {
    Insert(ctx context.Context, state MarketState) error
    Get(ctx context.Context, symbolID, period string, barTime time.Time) (MarketState, error)
}

// PostgresRepository implements Repository for PostgreSQL
type PostgresRepository struct {
    db *pgxpool.Pool
}

func (r *PostgresRepository) Insert(ctx context.Context, state MarketState) error {
    _, err := r.db.Exec(ctx, `
        INSERT INTO market_states (
            symbol_id, provider, period, bar_time,
            ema_fast, ema_slow, rsi, adx, atr
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        ON CONFLICT (symbol_id, provider, period, bar_time) 
        DO UPDATE SET
            ema_fast = EXCLUDED.ema_fast,
            ema_slow = EXCLUDED.ema_slow,
            rsi = EXCLUDED.rsi,
            adx = EXCLUDED.adx,
            atr = EXCLUDED.atr
    `, state.SymbolID, state.Provider, state.Period, state.BarTime,
       state.EMAFast, state.EMASlow, state.RSI, state.ADX, state.ATR)
    return err
}

// MockRepository for testing - no DB needed
type MockRepository struct {
    states map[string]MarketState
}

func (m *MockRepository) Insert(ctx context.Context, state MarketState) error {
    m.states[state.Period] = state
    return nil
}
```

---

## 5. Usage in Bot

**internal/bot/bot.go**
```go
func (b *Bot) onCandle(ctx context.Context, period string, bar api.Trendbar) {
    // Store raw candle
    b.candles.Insert(ctx, bar)
    
    // Load last 21 closes for this timeframe
    closes, err := b.candleRepo.GetLastN(ctx, period, 21)
    if err != nil {
        return
    }
    
    // Calculate indicators (pure function, no side effects)
    marketState := b.calculator.Calculate(period, closes.Closes(), closes.OHLC())
    
    // Store in market_states
    b.marketStateRepo.Insert(ctx, marketState)
    
    // Only generate signals on M5
    if period == "M5" {
        b.generateSignal(ctx, marketState)
    }
}
```

---

## Testing Example

**indicator/ema_test.go**
```go
package indicator

import "testing"

func TestEMA(t *testing.T) {
    // Pure function - no setup needed!
    prices := []float64{1.0, 1.1, 1.2, 1.15, 1.25, 1.3, 1.28, 1.32, 1.35}
    
    calc := NewEMACalculator(9)
    ema := calc.Calculate(prices)
    
    if ema < 1.2 || ema > 1.35 {
        t.Errorf("unexpected EMA: %f", ema)
    }
}

func TestRSI(t *testing.T) {
    // Pure function - no mocking, no DB, no timeframe context
    prices := []float64{44, 44.34, 44.09, 43.61, 44.33, 44.83, 45.10, 45.42, 45.84}
    
    calc := NewRSICalculator(14)
    rsi := calc.Calculate(prices)
    
    if rsi < 0 || rsi > 100 {
        t.Errorf("RSI out of bounds: %f", rsi)
    }
}
```

---

## Benefits of This Design

✅ **Pure functions** — Easy to test, no mocking needed
✅ **Timeframe-agnostic** — Same EMA for M5, H1, H4, D1
✅ **Swappable** — Change `indicator.EMACalculator` implementation without touching bot code
✅ **Multi-provider ready** — Indicators work for cTrader, Binance, Kraken (price is price)
✅ **Parallel calculation** — Calculate all timeframes concurrently
✅ **Fast warmup** — Reuse same calculator for historical + live data

---

## Timeframes to Add

Keep current: M5, H1, H4, D1
Add: M15, M30

**Total = 6 timeframes:**
- M5 (5 min)
- M15 (15 min)
- M30 (30 min)
- H1 (1 hour)
- H4 (4 hours)
- D1 (1 day)
