-- Add default SL/TP at symbol level so bot knows what to use per symbol.
ALTER TABLE symbol_configs
    ADD COLUMN default_sl_pips INT DEFAULT 10,
    ADD COLUMN default_tp_pips INT DEFAULT 20;

-- Track why trades were closed (tp_hit, sl_hit, manual, timeout, regime_change, etc).
ALTER TABLE fills ADD COLUMN close_reason TEXT;

-- Log every time SL/TP is adjusted during a trade (trailing stops, dynamic adjustments, etc).
CREATE TABLE position_adjustments (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    position_id     UUID            NOT NULL REFERENCES positions(id) ON DELETE CASCADE,

    old_sl          NUMERIC(12,5),  -- SL price before adjustment
    new_sl          NUMERIC(12,5),  -- SL price after adjustment
    old_tp          NUMERIC(12,5),  -- TP price before adjustment
    new_tp          NUMERIC(12,5),  -- TP price after adjustment

    reason          TEXT            NOT NULL,  -- trailing_stop, market_regime_change, manual, adx_threshold, breakeven

    adjusted_at     TIMESTAMPTZ     DEFAULT NOW()
);

CREATE INDEX idx_position_adjustments_position ON position_adjustments(position_id);
CREATE INDEX idx_position_adjustments_reason ON position_adjustments(reason);
