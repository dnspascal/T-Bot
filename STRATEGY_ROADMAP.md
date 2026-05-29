# Trading Bot Strategy Roadmap

## Phase 1: Foundation (Current - DONE)
- [x] EMA 9/21 crossover entry signals
- [x] RSI(14) filter to confirm entries (avoid counter-trend noise)
- [x] Basic position sizing based on 1% risk
- [x] Fixed stop loss (default 10 pips) and take profit (20 pips)
- [x] cTrader API integration
- [x] Position & trade logging
- [x] Multi-symbol support (database + lookup service)

**Current Bot Behavior:**
- Open position when EMA9 crosses EMA21 + RSI confirmation
- Hold until TP or SL hit (fixed levels)
- No early exit logic
- Issue: Exits at SL when market reverses, instead of exiting early when reversal signals appear

---

## Phase 2: Market Regime Detection + Dynamic Exit (NEXT)
Solve the bleeding losses issue by detecting when market direction is changing while holding a position.

### 2.1: Market Regime Tracking
- [ ] ADX (Average Directional Index, 14) to measure trend strength
  - ADX > 25 = strong trend (good entry conditions)
  - ADX < 20 = weak trend / ranging (avoid entries)
- [ ] ATR (Average True Range, 14) for volatility context
  - Use to scale position size dynamically
  - Use to adjust SL/TP distances based on volatility

### 2.2: Early Exit Signals
While holding an open position, monitor these signals:
- [ ] EMA crossback (9 crosses back below 21 on BUY, or above on SELL) = reversal
- [ ] RSI divergence (price makes new high but RSI doesn't)
- [ ] RSI leaving overbought/oversold after peak (+10 pips)
- [ ] ADX decline (trend weakening)
- [ ] Position drawdown threshold (e.g., peaked at +$1.80, now at +$1.00)

### 2.3: Exit Logic
- Exit immediately if: EMA crossback + ADX declining (2+ signals)
- Exit if: Drawdown from peak > threshold (e.g., 40% of max profit)
- Log exit reason in `fills.close_reason`: "ema_crossback", "drawdown", "adx_decline", etc.

### 2.4: Code Changes (In-Memory, No DB)
1. **Strategy enhancements:**
   - Add ADX(14) and ATR(14) calculation methods to `CombinedStrategy`
   - Add `CheckExitSignals(candle, openPosition)` method
   - Return `ExitReason` enum: `HOLD`, `EMA_CROSSBACK`, `DRAWDOWN`, `ADX_DECLINE`

2. **Bot changes:**
   - Track `positionPeakProfit` and `positionEntryTime` when position opens
   - On each tick, recalculate live P&L and drawdown %
   - Before entering new trade, call `CheckExitSignals()` on open position
   - If exit triggered, close position and set `fills.close_reason` before opening new one

3. **MarketState storage (optional for now):**
   - After each candle, insert ADX/ATR into `market_states` table
   - Query it for exit logic (can skip for Phase 2.1, calculate on-the-fly)

**Expected Outcome:** Reduce SL hits, exit positions before they reverse hard

---

## Phase 3: Trade Duration Filters
Prevent overtrading short-duration noise.

- [ ] Minimum candle hold: Don't exit in first 5 minutes
- [ ] Session awareness: Don't enter in last 30 min of session
- [ ] News calendar filter: Skip entries 1 hour before major events
- [ ] Daily reset: Track entries per day to limit overtrading

---

## Phase 4: Market Structure (Trend vs Range Detection)
Adapt strategy based on market regime.

- [ ] Higher timeframe structure (1H, 4H) to determine overall bias
- [ ] Don't short in uptrend, don't long in downtrend (on 1H)
- [ ] Range detection: Only take reversals in ranging markets
- [ ] Confluence scoring: Weight entries higher if they align with higher TF trend

---

## Phase 5: Risk Management Enhancements
- [ ] Partial take profits (exit 50% at TP1, scale out)
- [ ] Trailing stop loss (lock in 50% of profit, trail by ATR)
- [ ] Position sizing based on ADX (smaller in weak trends)
- [ ] Max drawdown circuit breaker per trading session

---

## Phase 6: Multi-Strategy Combination (Future)
- [ ] Bollinger Band + Support/Resistance confluence entries
- [ ] MACD confirmation for momentum
- [ ] Volume profile analysis
- [ ] Time-of-day bias (certain hours more profitable)

---

## Database (No Changes Needed)
Already have everything:
- `market_states` table stores ADX/ATR per candle (not in signals table — already rejected)
- `fills.close_reason` stores why position closed: `tp_hit`, `sl_hit`, `regime_change`, etc.
- `positions` table tracks open positions (P&L calculated in-memory per tick)

---

## Implementation Priority
1. **Phase 2.1-2.3** (Dynamic Exit) — addresses your current $1.80→$1.00 bleed issue
2. **Phase 3** (Trade Duration) — prevents revenge trading
3. **Phase 4** (Market Structure) — improves win rate
4. Phases 5-6 are optimization layers

## Why Phase-by-Phase?
- Each phase is testable independently
- Can measure impact of each feature on P&L
- Easier to debug if something breaks
- Can ship improvements incrementally without waiting for "complete" rewrite
