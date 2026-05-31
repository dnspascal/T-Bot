-- Every fill (execution) event from the provider.
-- One order can produce multiple fills (partial fills, close events).
-- This is the ground truth of what actually happened financially.
CREATE TABLE fills (
    id                   UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    our_order_id         UUID          REFERENCES orders(id),    -- our internal order record
    our_position_id      UUID          REFERENCES positions(id), -- our internal position record
    provider             TEXT          NOT NULL DEFAULT 'ctrader', -- ctrader | oanda | ib | mt5
    provider_fill_id     TEXT          NOT NULL,   -- provider's own fill/deal identifier
    provider_order_id    TEXT,                     -- provider's own order identifier
    provider_position_id TEXT,                     -- provider's own position identifier
    symbol_id            UUID          NOT NULL REFERENCES symbols(id),
    side                 TEXT          NOT NULL CHECK (side IN ('BUY', 'SELL')),

    -- Fill details
    volume               BIGINT,        -- requested volume in provider units
    filled_volume        BIGINT,        -- actually filled (may be less — partial fill)
    execution_price      NUMERIC(12,5), -- exact price provider filled at
    event_type           TEXT          NOT NULL,   -- free-form, provider-specific (e.g. ORDER_FILLED)
    fill_status          TEXT,                     -- free-form, provider-specific

    -- Costs for this fill (real currency amounts)
    commission           NUMERIC(18,4),
    margin_rate          NUMERIC(12,8), -- margin rate at fill time
    base_to_usd_rate     NUMERIC(12,8), -- currency conversion rate used

    -- Close position detail (populated only when this fill CLOSES a position)
    close_entry_price    NUMERIC(12,5), -- what the original entry price was
    close_reason         TEXT,          -- why this position closed: tp_hit, sl_hit, manual, timeout, regime_change
    gross_profit         NUMERIC(18,4), -- raw profit before swap/commission
    close_swap           NUMERIC(18,4), -- accumulated swap on this closed volume
    close_commission     NUMERIC(18,4), -- accumulated commission on this closed volume
    balance_after        NUMERIC(18,4), -- account balance immediately after this fill
    closed_volume        BIGINT,        -- how much of the position was closed
    pnl_conversion_fee   NUMERIC(18,4), -- fee for converting PnL to deposit currency
    trade_duration_ms    BIGINT,        -- ms from position open to this close fill
    -- cTrader encodes commission/swap/fee as sint64 (negative = cost).
    -- We store them as-is (negative), so net = gross + commission + swap + fee.
    net_profit           NUMERIC(18,4) GENERATED ALWAYS AS (
                             COALESCE(gross_profit, 0)
                             + COALESCE(close_commission, 0)
                             + COALESCE(close_swap, 0)
                             + COALESCE(pnl_conversion_fee, 0)
                         ) STORED,

    -- Provider-side timestamps (different from our received_at)
    provider_create_time TIMESTAMPTZ,
    provider_exec_time   TIMESTAMPTZ,

    raw_payload          BYTEA,         -- full provider response bytes, nothing discarded

    received_at          TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    UNIQUE (provider, provider_fill_id)
);

CREATE INDEX idx_fills_our_order_id    ON fills (our_order_id);
CREATE INDEX idx_fills_our_position_id ON fills (our_position_id);
CREATE INDEX idx_fills_provider_pos    ON fills (provider, provider_position_id);
CREATE INDEX idx_fills_symbol          ON fills (symbol_id, received_at DESC);
CREATE INDEX idx_fills_event_type      ON fills (event_type, received_at DESC);
