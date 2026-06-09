-- Trading decision record: what action we decided to take and why.
-- One row per decision (BUY/SELL/HOLD) when M5 candle closes.
CREATE TABLE signals (
    id              UUID            NOT NULL DEFAULT gen_random_uuid(),
    symbol_id       UUID            NOT NULL REFERENCES symbols(id),
    provider        TEXT            NOT NULL DEFAULT 'ctrader',

    -- The decision
    signal          TEXT            NOT NULL CHECK (signal IN ('BUY', 'SELL', 'HOLD')),

    -- Snapshot of indicator values per timeframe at decision time.
    -- market_state_id references the market_states row (no FK — both are hypertables).
    checked_market_states JSONB,

    -- Quality metrics
    confluence      INT             DEFAULT 0,  -- how many timeframes/factors agreed (>=2 required to trade)
    confidence      NUMERIC(5,2),               -- 0.0-1.0: estimated win probability (from backtest/historical)

    -- Performance
    processing_us   BIGINT          NOT NULL DEFAULT 0,  -- microseconds to evaluate this signal

    -- Timing
    bar_time        TIMESTAMPTZ,                          -- when the M5 candle that triggered this closed (nullable: set when available)
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id, created_at)
);

SELECT create_hypertable('signals', 'created_at');

CREATE INDEX idx_signals_symbol     ON signals(symbol_id, created_at DESC);
CREATE INDEX idx_signals_actionable ON signals(symbol_id, created_at DESC) WHERE signal != 'HOLD';
CREATE INDEX idx_signals_confluence ON signals(symbol_id, confluence DESC);
CREATE INDEX idx_signals_confidence ON signals(symbol_id, confidence DESC) WHERE confidence IS NOT NULL;
CREATE INDEX idx_signals_jsonb      ON signals USING GIN(checked_market_states);
