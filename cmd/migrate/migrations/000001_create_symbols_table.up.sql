CREATE TABLE symbols (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    symbol          TEXT            NOT NULL UNIQUE,
    asset_class     TEXT            NOT NULL,  -- forex, crypto, indices, commodities, stocks
    base_asset      TEXT,
    quote_asset     TEXT,
    exchange        TEXT            NOT NULL,
    exchange_symbol_id TEXT,

    created_at      TIMESTAMPTZ     DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,    -- soft delete: NULL = active

    UNIQUE(exchange, symbol),
    CHECK (deleted_at IS NULL OR updated_at <= deleted_at)
);

CREATE INDEX idx_symbols_exchange ON symbols(exchange);
CREATE INDEX idx_symbols_active ON symbols(exchange) WHERE deleted_at IS NULL;
