-- Periodic snapshots of account state received from the provider.
-- Captured on startup and after every trade so balance and leverage
-- can be reconstructed at any point in history.
-- All monetary values stored as real currency amounts (e.g. 50.0000 = $50).
CREATE TABLE account_snapshots (
    id               UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    provider         TEXT          NOT NULL DEFAULT 'ctrader', -- ctrader | oanda | ib | mt5
    provider_acct_id TEXT          NOT NULL,   -- provider's own account identifier

    balance          NUMERIC(18,4) NOT NULL,   -- real value in deposit currency
    leverage_ratio   NUMERIC(8,2),             -- e.g. 100.00 = 100x, 500.00 = 500x
    max_leverage     NUMERIC(8,2),
    account_mode     TEXT,                     -- hedged | netted | other
    currency         TEXT,                     -- deposit currency e.g. USD, EUR
    broker_name      TEXT,

    -- Provider-specific flags stored as generic booleans
    is_limited_risk  BOOL,
    fair_stop_out    BOOL,

    -- Full provider response for reference
    provider_payload JSONB,

    trigger          TEXT,                     -- startup | post_trade | scheduled

    snapshotted_at   TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_account_snapshots ON account_snapshots (provider, provider_acct_id, snapshotted_at DESC);
