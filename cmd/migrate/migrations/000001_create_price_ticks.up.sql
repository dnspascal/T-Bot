CREATE EXTENSION IF NOT EXISTS timescaledb;

-- Every raw price quote received from the broker.
-- bid and ask are stored; mid and spread are derived automatically.
CREATE TABLE price_ticks (
    id               UUID          NOT NULL DEFAULT gen_random_uuid(),
    symbol           TEXT          NOT NULL,
    symbol_id        BIGINT        NOT NULL,
    bid              NUMERIC(12,5) NOT NULL,
    ask              NUMERIC(12,5) NOT NULL,
    mid              NUMERIC(12,5) NOT NULL GENERATED ALWAYS AS ((bid + ask) / 2) STORED,
    spread           NUMERIC(8,5)  NOT NULL GENERATED ALWAYS AS (ask - bid) STORED,
    session_close    NUMERIC(12,5),           -- closing price of the current session if sent
    provider_timestamp TIMESTAMPTZ,           -- timestamp inside the provider's own message
    received_at      TIMESTAMPTZ   NOT NULL DEFAULT NOW(), -- when our bot received this tick
    processing_ms    BIGINT        NOT NULL DEFAULT 0,     -- received_at → stored in DB
    PRIMARY KEY (id, received_at)
);

SELECT create_hypertable('price_ticks', 'received_at');

CREATE INDEX idx_price_ticks_symbol ON price_ticks (symbol, received_at DESC);
