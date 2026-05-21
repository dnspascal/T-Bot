-- OHLCV candle data sent by the broker inside spot events (ProtoOATrendbar).
-- Stored per symbol per timeframe period.
CREATE TABLE candles (
    id          UUID          NOT NULL DEFAULT gen_random_uuid(),
    symbol      TEXT          NOT NULL,
    symbol_id   BIGINT        NOT NULL,
    period      TEXT          NOT NULL, -- M1 M5 M15 M30 H1 H4 D1 W1 MN1
    open        NUMERIC(12,5) NOT NULL,
    high        NUMERIC(12,5) NOT NULL,
    low         NUMERIC(12,5) NOT NULL,
    close       NUMERIC(12,5) NOT NULL,
    tick_volume BIGINT        NOT NULL DEFAULT 0,
    bar_time    TIMESTAMPTZ   NOT NULL, -- when this bar opened (from utcTimestampInMinutes)
    received_at TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, bar_time)
);

SELECT create_hypertable('candles', 'bar_time');

CREATE UNIQUE INDEX idx_candles_unique   ON candles (symbol, period, bar_time); -- bar_time is partition col ✓
CREATE        INDEX idx_candles_symbol   ON candles (symbol, period, bar_time DESC);
