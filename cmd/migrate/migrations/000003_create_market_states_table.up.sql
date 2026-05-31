-- Market context at each candle: all indicators describing what the market is doing.
-- One row per completed candle per timeframe (M5, H1, H4, D1) per provider per symbol.
-- Source of truth for market state; signals table references this to show WHY decisions were made.
CREATE TABLE market_states (
    id              UUID            NOT NULL DEFAULT gen_random_uuid(),
    symbol_id       UUID            NOT NULL REFERENCES symbols(id),
    provider        TEXT            NOT NULL DEFAULT 'ctrader',  -- ctrader, binance, oanda, etc.
    period          TEXT            NOT NULL,  -- M5, H1, H4, D1

    -- OHLCV (price action)
    open            NUMERIC(12,5),
    high            NUMERIC(12,5),
    low             NUMERIC(12,5),
    close           NUMERIC(12,5),
    volume          BIGINT,

    -- Core indicators (decision inputs)
    ema_fast        NUMERIC(12,5),              -- EMA(9): fast trend
    ema_slow        NUMERIC(12,5),              -- EMA(21): slow trend
    rsi             NUMERIC(6,2),               -- RSI(14): momentum (0-100)
    adx             NUMERIC(6,2),               -- ADX(14): trend strength (0-100)
    atr             NUMERIC(12,5),              -- ATR(14): volatility

    -- Structure (support/resistance)
    support_level   NUMERIC(12,5),              -- strategic support where price bounces up
    resistance_level NUMERIC(12,5),             -- strategic resistance where price bounces down
    trend_high      NUMERIC(12,5),              -- highest point during current trend
    trend_low       NUMERIC(12,5),              -- lowest point during current trend
    breakout_level  NUMERIC(12,5),              -- price level that was just broken (breakout trigger)

    -- Regime classification
    regime          TEXT,                       -- trending_up, trending_down, ranging, breakout, unknown
    volatility_trend TEXT,                      -- expanding, contracting, stable
    momentum_direction TEXT,                    -- rising, falling, stable

    -- Volume context
    volume_ma       BIGINT,                     -- volume moving average

    -- Performance monitoring
    processing_ms   BIGINT DEFAULT 0,           -- time to calculate all indicators (received bar → stored state)

    -- Flexible storage for optional indicators (no schema change needed)
    -- Example: {"bb_upper": 1.17, "bb_lower": 1.16, "macd": 0.003, "stoch_k": 75}
    indicators      JSONB,

    -- Timing
    bar_time        TIMESTAMPTZ     NOT NULL,  -- when this bar opened (partition column)
    created_at      TIMESTAMPTZ     DEFAULT NOW(),

    PRIMARY KEY (id, bar_time),
    UNIQUE(symbol_id, provider, period, bar_time)
);

SELECT create_hypertable('market_states', 'bar_time');

CREATE INDEX idx_market_states_symbol ON market_states(symbol_id, provider, period, bar_time DESC);
CREATE INDEX idx_market_states_regime ON market_states(symbol_id, regime, bar_time DESC) WHERE regime IS NOT NULL;
CREATE INDEX idx_market_states_adx ON market_states(symbol_id, adx, bar_time DESC);
CREATE INDEX idx_market_states_rsi ON market_states(symbol_id, rsi, bar_time DESC);
