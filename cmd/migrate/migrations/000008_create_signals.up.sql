-- Trading decision record: what action we decided to take and why.
-- One row per decision (BUY/SELL/HOLD) when M5 candle closes.
-- Stores references to market_states across timeframes in flexible JSONB (scales to any timeframe combo).
CREATE TABLE signals (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol_id       UUID            NOT NULL REFERENCES symbols(id),
    provider        TEXT            NOT NULL DEFAULT 'ctrader',

    -- The decision
    signal          TEXT            NOT NULL CHECK (signal IN ('BUY', 'SELL', 'HOLD')),

    -- Market states checked across timeframes (flexible, scalable)
    -- Example:
    -- {
    --   "M5": {"market_state_id": "uuid-m5", "regime": "trending_up", "adx": 28},
    --   "H1": {"market_state_id": "uuid-h1", "regime": "trending_up", "adx": 32},
    --   "H4": {"market_state_id": "uuid-h4", "regime": "ranging", "adx": 18},
    --   "D1": {"market_state_id": "uuid-d1", "regime": "trending_up", "adx": 40}
    -- }
    checked_market_states JSONB,

    -- Quality metrics
    confluence      INT             DEFAULT 0,  -- 0-N: how many timeframes agreed
    -- 0 = no conviction (HOLD)
    -- 1 = weak signal (only 1 timeframe/indicator agreed)
    -- 2 = moderate (2 timeframes agreed)
    -- 3 = strong (3 timeframes agreed)
    -- 4+ = very strong (4+ timeframes agreed)

    confidence      NUMERIC(5,2),               -- 0.0-1.0: estimated win probability (from backtest/historical)

    -- Timing
    bar_time        TIMESTAMPTZ     NOT NULL,  -- when M5 candle closed (triggered this signal)
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    UNIQUE(symbol_id, provider, bar_time)
);

SELECT create_hypertable('signals', 'created_at');

CREATE INDEX idx_signals_symbol     ON signals(symbol_id, created_at DESC);
CREATE INDEX idx_signals_actionable ON signals(symbol_id, created_at DESC) WHERE signal != 'HOLD';
CREATE INDEX idx_signals_confluence ON signals(symbol_id, confluence DESC);
CREATE INDEX idx_signals_confidence ON signals(symbol_id, confidence DESC) WHERE confidence IS NOT NULL;
CREATE INDEX idx_signals_jsonb      ON signals USING GIN(checked_market_states);
