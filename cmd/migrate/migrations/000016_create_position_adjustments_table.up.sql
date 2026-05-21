-- Log every SL/TP adjustment during a trade (trailing stops, dynamic adjustments, etc).
-- Lets you analyze: which adjustments improved win rate? Which hurt it?
CREATE TABLE position_adjustments (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    position_id     UUID            NOT NULL REFERENCES positions(id) ON DELETE CASCADE,

    old_sl          NUMERIC(12,5),  -- stop loss price before adjustment
    new_sl          NUMERIC(12,5),  -- stop loss price after adjustment
    old_tp          NUMERIC(12,5),  -- take profit price before adjustment
    new_tp          NUMERIC(12,5),  -- take profit price after adjustment

    reason          TEXT            NOT NULL,  -- trailing_stop, market_regime_change, manual, adx_threshold, breakeven

    adjusted_at     TIMESTAMPTZ     DEFAULT NOW()
);

CREATE INDEX idx_position_adjustments_position ON position_adjustments(position_id);
CREATE INDEX idx_position_adjustments_reason ON position_adjustments(reason, adjusted_at DESC);
