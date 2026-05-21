-- One row per trading day, updated after every closed trade.
-- Survives bot restarts — loaded on startup to restore risk manager state.
CREATE TABLE daily_pnl (
    id                  UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    date                DATE          NOT NULL,
    symbol              TEXT          NOT NULL DEFAULT 'EURUSD',

    -- P&L breakdown (real currency amounts in deposit currency)
    realized_pnl        NUMERIC(18,4) NOT NULL DEFAULT 0, -- net after commission and swap
    gross_profit        NUMERIC(18,4) NOT NULL DEFAULT 0, -- raw profit before costs
    total_commission    NUMERIC(18,4) NOT NULL DEFAULT 0, -- total commission paid today
    total_swap          NUMERIC(18,4) NOT NULL DEFAULT 0, -- total swap paid today

    -- Trade counts
    trade_count         INT           NOT NULL DEFAULT 0,
    win_count           INT           NOT NULL DEFAULT 0,
    loss_count          INT           NOT NULL DEFAULT 0,

    -- Latency stats for the day
    avg_round_trip_ms   BIGINT        NOT NULL DEFAULT 0,  -- average order round-trip
    avg_slippage_points BIGINT        NOT NULL DEFAULT 0,  -- average slippage in points

    updated_at          TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    UNIQUE (date, symbol)
);

CREATE INDEX idx_daily_pnl_date ON daily_pnl (date DESC);
