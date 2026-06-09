-- Every order our bot attempts to place.
-- Created before sending to provider so we have a record even if the send fails.
-- provider_order_id and provider_position_id are populated when execution event arrives.
CREATE TABLE orders (
    id                    UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    signal_id             UUID,          -- signal that triggered this order (no FK — signals is a hypertable)
    provider              TEXT          NOT NULL DEFAULT 'ctrader', -- ctrader | oanda | ib | mt5

    -- Provider IDs populated from execution event
    provider_order_id     TEXT,          -- provider's own order identifier
    provider_position_id  TEXT,          -- provider's own position identifier

    symbol_id             UUID          NOT NULL REFERENCES symbols(id),
    side                  TEXT          NOT NULL CHECK (side IN ('BUY', 'SELL')),
    volume                BIGINT        NOT NULL,   -- requested provider units
    sl                    NUMERIC(12,5),
    tp                    NUMERIC(12,5),
    entry_price           NUMERIC(12,5),            -- actual fill price (from execution event)
    slippage_points       BIGINT,                   -- slippage in points if reported by provider

    status                TEXT          NOT NULL DEFAULT 'pending',
    error_code            TEXT,          -- provider error code if rejected
    error_msg             TEXT,

    -- Full latency trail
    sent_at               TIMESTAMPTZ,  -- when we dispatched to provider
    execution_received_at TIMESTAMPTZ,  -- when provider confirmed execution
    round_trip_ms         BIGINT,       -- sent_at → execution_received_at

    created_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_orders_status           ON orders (status);
CREATE INDEX idx_orders_symbol_id        ON orders (symbol_id, created_at DESC);
CREATE INDEX idx_orders_signal           ON orders (signal_id);
CREATE INDEX idx_orders_provider_order   ON orders (provider, provider_order_id);
CREATE INDEX idx_orders_provider_pos     ON orders (provider, provider_position_id);
