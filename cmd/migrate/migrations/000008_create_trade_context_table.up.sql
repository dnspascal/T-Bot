-- Snapshot of market and account state at each trade execution.
-- Links every trade to: the signal that triggered it, the market regime when it was placed, the account state.
-- Essential for backtesting: "which trades worked in trending vs ranging markets?"
CREATE TABLE trade_context (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id            UUID            NOT NULL REFERENCES orders(id) ON DELETE CASCADE,

    signal_id           UUID,           -- signal that triggered this order (signals is hypertable, no FK)
    market_state_id     UUID,           -- market regime snapshot at order time (reference only, no FK due to TimescaleDB partitioning)

    -- Account snapshot at execution
    balance_before      NUMERIC(18,4),  -- balance before this trade
    equity_before       NUMERIC(18,4),  -- equity before this trade
    margin_used         NUMERIC(18,4),  -- margin required for this position

    created_at          TIMESTAMPTZ     DEFAULT NOW()
);

CREATE INDEX idx_trade_context_order ON trade_context(order_id);
CREATE INDEX idx_trade_context_signal ON trade_context(signal_id);
CREATE INDEX idx_trade_context_market ON trade_context(market_state_id);
