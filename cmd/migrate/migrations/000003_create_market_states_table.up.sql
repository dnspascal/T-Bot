-- Market regime context: trending, ranging, breakout, volatility levels.
-- Separate from signals table — describes market state, not trading decisions.
-- One row per completed candle (M5, H1, H4, D1) with indicators and boundaries.
CREATE TABLE market_states (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol_id       UUID            NOT NULL REFERENCES symbols(id),
    period          TEXT            NOT NULL,  -- M5, H1, H4, D1

    -- Core regime indicators (regime, not signal)
    atr             NUMERIC(10,5),              -- volatility measure
    adx             NUMERIC(6,2),               -- trend strength (0-100)

    -- Market state classification
    state           TEXT,                       -- trending_up, trending_down, ranging, breakout, unknown

    -- Regime boundaries (only set when relevant to state)
    range_high      NUMERIC(12,5),              -- upper bound when ranging
    range_low       NUMERIC(12,5),              -- lower bound when ranging
    trend_high      NUMERIC(12,5),              -- highest point during trend
    trend_low       NUMERIC(12,5),              -- lowest point during trend
    breakout_level  NUMERIC(12,5),              -- price level that was broken

    -- Flexible indicator storage for future additions
    -- E.g. {"bb_width": 120.5, "stoch_k": 85, "volume_profile": "bullish"}
    indicators      JSONB,

    -- Timing
    bar_time        TIMESTAMPTZ     NOT NULL,  -- when this bar opened (partition column)
    created_at      TIMESTAMPTZ     DEFAULT NOW(),

    PRIMARY KEY (id, bar_time),
    UNIQUE(symbol_id, period, bar_time)
);

SELECT create_hypertable('market_states', 'bar_time');

CREATE INDEX idx_market_states_symbol ON market_states(symbol_id, period, bar_time DESC);
CREATE INDEX idx_market_states_state  ON market_states(symbol_id, state, bar_time DESC) WHERE state != 'unknown';
