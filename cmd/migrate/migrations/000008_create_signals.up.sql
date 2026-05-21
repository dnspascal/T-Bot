-- Every decision the strategy engine made, including HOLDs.
-- Full record means you can replay exactly what the bot was thinking at any moment.
CREATE TABLE signals (
    id            UUID          NOT NULL DEFAULT gen_random_uuid(),
    symbol_id     UUID          NOT NULL REFERENCES symbols(id),
    signal        TEXT          NOT NULL CHECK (signal IN ('BUY', 'SELL', 'HOLD')),
    fast_ema      NUMERIC(12,5) NOT NULL, -- EMA(9)  value at decision time
    slow_ema      NUMERIC(12,5) NOT NULL, -- EMA(21) value at decision time
    rsi           NUMERIC(6,2)  NOT NULL DEFAULT 50, -- RSI(14) value at decision time
    confluence    INT           NOT NULL DEFAULT 0,  -- 0=hold 1=weak 2=strong
    price_mid     NUMERIC(12,5) NOT NULL, -- candle close price that triggered this signal
    processing_ms BIGINT        NOT NULL DEFAULT 0, -- time from candle close to signal stored
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
);

SELECT create_hypertable('signals', 'created_at');

CREATE INDEX idx_signals_symbol     ON signals (symbol_id, created_at DESC);
CREATE INDEX idx_signals_actionable ON signals (symbol_id, created_at DESC) WHERE signal != 'HOLD';
