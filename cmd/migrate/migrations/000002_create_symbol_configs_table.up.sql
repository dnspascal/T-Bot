CREATE TABLE symbol_configs (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol_id       UUID            NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,

    pip_size        NUMERIC(10,8)   NOT NULL,
    min_volume      INT64           NOT NULL,
    lot_size        INT64           NOT NULL,
    max_volume      INT64,          -- maximum units per single order (NULL = no limit)
    max_daily_volume INT64,         -- maximum units per day (NULL = no limit)
    trading_hours   TEXT,
    is_active       BOOLEAN         DEFAULT true,

    default_sl_pips INT             DEFAULT 10,   -- default stop loss in pips for this symbol
    default_tp_pips INT             DEFAULT 20,   -- default take profit in pips for this symbol

    created_at      TIMESTAMPTZ     DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,    -- soft delete: NULL = active

    UNIQUE(symbol_id),
    CHECK (deleted_at IS NULL OR updated_at <= deleted_at)
);

CREATE INDEX idx_symbol_configs_symbol ON symbol_configs(symbol_id);
CREATE INDEX idx_symbol_configs_active ON symbol_configs(symbol_id) WHERE deleted_at IS NULL;
