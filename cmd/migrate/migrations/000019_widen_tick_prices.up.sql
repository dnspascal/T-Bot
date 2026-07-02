-- Widen all price/level columns from NUMERIC(12,5) to NUMERIC(18,5) to support
-- instruments with large nominal prices (gold ~3300, indices, crypto).
-- NUMERIC(18,5) allows up to 9,999,999,999,999.99999 — future-proof.

-- price_ticks: generated columns must be dropped and recreated
ALTER TABLE price_ticks DROP COLUMN mid;
ALTER TABLE price_ticks DROP COLUMN spread;
ALTER TABLE price_ticks
    ALTER COLUMN bid           TYPE NUMERIC(18,5),
    ALTER COLUMN ask           TYPE NUMERIC(18,5),
    ALTER COLUMN session_close TYPE NUMERIC(18,5);
ALTER TABLE price_ticks
    ADD COLUMN mid    NUMERIC(18,5) GENERATED ALWAYS AS ((bid + ask) / 2) STORED,
    ADD COLUMN spread NUMERIC(12,5) GENERATED ALWAYS AS (ask - bid) STORED;

-- candles
ALTER TABLE candles
    ALTER COLUMN open  TYPE NUMERIC(18,5),
    ALTER COLUMN high  TYPE NUMERIC(18,5),
    ALTER COLUMN low   TYPE NUMERIC(18,5),
    ALTER COLUMN close TYPE NUMERIC(18,5);

-- market_states
ALTER TABLE market_states
    ALTER COLUMN open             TYPE NUMERIC(18,5),
    ALTER COLUMN high             TYPE NUMERIC(18,5),
    ALTER COLUMN low              TYPE NUMERIC(18,5),
    ALTER COLUMN close            TYPE NUMERIC(18,5),
    ALTER COLUMN ema_fast         TYPE NUMERIC(18,5),
    ALTER COLUMN ema_slow         TYPE NUMERIC(18,5),
    ALTER COLUMN atr              TYPE NUMERIC(18,5),
    ALTER COLUMN support_level    TYPE NUMERIC(18,5),
    ALTER COLUMN resistance_level TYPE NUMERIC(18,5),
    ALTER COLUMN trend_high       TYPE NUMERIC(18,5),
    ALTER COLUMN trend_low        TYPE NUMERIC(18,5),
    ALTER COLUMN breakout_level   TYPE NUMERIC(18,5);

-- orders
ALTER TABLE orders
    ALTER COLUMN sl          TYPE NUMERIC(18,5),
    ALTER COLUMN tp          TYPE NUMERIC(18,5),
    ALTER COLUMN entry_price TYPE NUMERIC(18,5);

-- positions
ALTER TABLE positions
    ALTER COLUMN open_price    TYPE NUMERIC(18,5),
    ALTER COLUMN current_sl    TYPE NUMERIC(18,5),
    ALTER COLUMN current_tp    TYPE NUMERIC(18,5),
    ALTER COLUMN max_favorable TYPE NUMERIC(18,5),
    ALTER COLUMN max_adverse   TYPE NUMERIC(18,5);

-- fills
ALTER TABLE fills
    ALTER COLUMN execution_price  TYPE NUMERIC(18,5),
    ALTER COLUMN close_entry_price TYPE NUMERIC(18,5);

-- position_adjustments
ALTER TABLE position_adjustments
    ALTER COLUMN old_sl TYPE NUMERIC(18,5),
    ALTER COLUMN new_sl TYPE NUMERIC(18,5),
    ALTER COLUMN old_tp TYPE NUMERIC(18,5),
    ALTER COLUMN new_tp TYPE NUMERIC(18,5);
