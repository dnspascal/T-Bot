-- Test 3: M30 counter-trend block
-- Question: does blocking entries where M30 is trending AGAINST the trade
-- direction improve win rate? Both losses on 2026-07-06 had M30 trending_down
-- while the bot entered BUY.
--
-- Two tests:
--   A) Standalone: apply only the M30 block to all 1,361 base setups
--   B) Combined:  add M30 block to the current strategy (leave-one-out style)
--
-- "won" = price moved >= 1 ATR in signal direction within 60 minutes.
-- Breakeven win rate at 1.67 R:R = 37.5%
--
-- Run: ssh makini "psql 'DB_URL'" < test3_m30_counter_trend.sql

WITH m5_base AS (
    SELECT
        ms.id, ms.symbol_id, ms.bar_time,
        ms.adx AS m5_adx, ms.rsi AS m5_rsi, ms.atr AS m5_atr,
        ms.volume AS m5_vol, ms.volume_ma AS m5_vol_ma,
        ms.support_level, ms.resistance_level, ms.ema_fast, ms.ema_slow,
        CASE
            WHEN ms.regime = 'trending_up'                             THEN 'BUY'
            WHEN ms.regime = 'trending_down'                           THEN 'SELL'
            WHEN ms.regime = 'breakout' AND ms.ema_fast >= ms.ema_slow THEN 'BUY'
            WHEN ms.regime = 'breakout' AND ms.ema_fast <  ms.ema_slow THEN 'SELL'
        END AS direction,
        c.close AS entry_price
    FROM market_states ms
    JOIN candles c ON c.symbol_id = ms.symbol_id AND c.period = 'M5' AND c.bar_time = ms.bar_time
    WHERE ms.period = 'M5'
      AND ms.regime IN ('trending_up','trending_down','breakout')
      AND ms.bar_time >= NOW() - INTERVAL '14 days'
      AND ms.bar_time < NOW()
      AND ms.atr > 0
),
with_outcome AS (
    SELECT b.*,
        EXTRACT(HOUR FROM b.bar_time AT TIME ZONE 'UTC') AS utc_hour,
        CASE
            WHEN b.direction = 'BUY' THEN EXISTS (
                SELECT 1 FROM candles fc
                WHERE fc.symbol_id = b.symbol_id AND fc.period = 'M5'
                  AND fc.bar_time > b.bar_time
                  AND fc.bar_time <= b.bar_time + INTERVAL '60 minutes'
                  AND fc.high >= b.entry_price + b.m5_atr
            )
            WHEN b.direction = 'SELL' THEN EXISTS (
                SELECT 1 FROM candles fc
                WHERE fc.symbol_id = b.symbol_id AND fc.period = 'M5'
                  AND fc.bar_time > b.bar_time
                  AND fc.bar_time <= b.bar_time + INTERVAL '60 minutes'
                  AND fc.low <= b.entry_price - b.m5_atr
            )
            ELSE false
        END AS won
    FROM m5_base b
),
with_htf AS (
    SELECT o.*,
        m15.regime AS m15_regime,
        m30.regime AS m30_regime,
        m30.adx    AS m30_adx,
        h1.regime  AS h1_regime,
        h1.adx     AS h1_adx
    FROM with_outcome o
    LEFT JOIN LATERAL (SELECT regime FROM market_states WHERE symbol_id = o.symbol_id AND period = 'M15' AND bar_time <= o.bar_time ORDER BY bar_time DESC LIMIT 1) m15 ON true
    LEFT JOIN LATERAL (SELECT regime, adx FROM market_states WHERE symbol_id = o.symbol_id AND period = 'M30' AND bar_time <= o.bar_time ORDER BY bar_time DESC LIMIT 1) m30 ON true
    LEFT JOIN LATERAL (SELECT regime, adx FROM market_states WHERE symbol_id = o.symbol_id AND period = 'H1'  AND bar_time <= o.bar_time ORDER BY bar_time DESC LIMIT 1) h1  ON true
),
agg AS (
    SELECT
        -- 00. Base: all 1,361 setups
        COUNT(*) AS s00, SUM(CAST(won AS int)) AS w00,

        -- 01. M30 not counter-trend (standalone, applied to all 1,361)
        --     Block BUY when M30 trending_down. Block SELL when M30 trending_up.
        COUNT(*) FILTER (WHERE
            NOT (direction = 'BUY'  AND m30_regime = 'trending_down')
            AND NOT (direction = 'SELL' AND m30_regime = 'trending_up')
        ) AS s01,
        SUM(CAST(won AS int)) FILTER (WHERE
            NOT (direction = 'BUY'  AND m30_regime = 'trending_down')
            AND NOT (direction = 'SELL' AND m30_regime = 'trending_up')
        ) AS w01,

        -- 02. M30 not counter-trend with ADX threshold (M30 ADX > 25 to be "strong")
        --     Softer version: only block if M30 trend has real strength
        COUNT(*) FILTER (WHERE
            NOT (direction = 'BUY'  AND m30_regime = 'trending_down' AND m30_adx > 25)
            AND NOT (direction = 'SELL' AND m30_regime = 'trending_up'  AND m30_adx > 25)
        ) AS s02,
        SUM(CAST(won AS int)) FILTER (WHERE
            NOT (direction = 'BUY'  AND m30_regime = 'trending_down' AND m30_adx > 25)
            AND NOT (direction = 'SELL' AND m30_regime = 'trending_up'  AND m30_adx > 25)
        ) AS w02,

        -- 03. H1 not counter-trend (standalone)
        COUNT(*) FILTER (WHERE
            NOT (direction = 'BUY'  AND h1_regime = 'trending_down')
            AND NOT (direction = 'SELL' AND h1_regime = 'trending_up')
        ) AS s03,
        SUM(CAST(won AS int)) FILTER (WHERE
            NOT (direction = 'BUY'  AND h1_regime = 'trending_down')
            AND NOT (direction = 'SELL' AND h1_regime = 'trending_up')
        ) AS w03,

        -- 04. Current strategy (baseline for comparison)
        COUNT(*) FILTER (WHERE
            ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
        ) AS s04,
        SUM(CAST(won AS int)) FILTER (WHERE
            ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
        ) AS w04,

        -- 05. Current strategy + M30 not counter-trend
        COUNT(*) FILTER (WHERE
            ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
            AND NOT (direction = 'BUY'  AND m30_regime = 'trending_down')
            AND NOT (direction = 'SELL' AND m30_regime = 'trending_up')
        ) AS s05,
        SUM(CAST(won AS int)) FILTER (WHERE
            ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
            AND NOT (direction = 'BUY'  AND m30_regime = 'trending_down')
            AND NOT (direction = 'SELL' AND m30_regime = 'trending_up')
        ) AS w05,

        -- 06. Current strategy + M30 not counter-trend (ADX > 25 threshold)
        COUNT(*) FILTER (WHERE
            ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
            AND NOT (direction = 'BUY'  AND m30_regime = 'trending_down' AND m30_adx > 25)
            AND NOT (direction = 'SELL' AND m30_regime = 'trending_up'   AND m30_adx > 25)
        ) AS s06,
        SUM(CAST(won AS int)) FILTER (WHERE
            ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
            AND NOT (direction = 'BUY'  AND m30_regime = 'trending_down' AND m30_adx > 25)
            AND NOT (direction = 'SELL' AND m30_regime = 'trending_up'   AND m30_adx > 25)
        ) AS w06
    FROM with_htf
)
SELECT
    unnest(ARRAY[
        '00. Base (no filters)',
        '01. M30 not counter-trend (standalone)',
        '02. M30 not counter-trend ADX>25 (standalone)',
        '03. H1 not counter-trend (standalone)',
        '04. Current strategy (baseline)',
        '05. Current strategy + M30 block',
        '06. Current strategy + M30 block ADX>25'
    ]) AS condition,
    unnest(ARRAY[s00,s01,s02,s03,s04,s05,s06]) AS setups,
    unnest(ARRAY[w00,w01,w02,w03,w04,w05,w06]) AS wins,
    unnest(ARRAY[
        s00-w00, s01-w01, s02-w02, s03-w03, s04-w04, s05-w05, s06-w06
    ]) AS losses,
    unnest(ARRAY[
        ROUND(100.0*w00/NULLIF(s00,0),1),
        ROUND(100.0*w01/NULLIF(s01,0),1),
        ROUND(100.0*w02/NULLIF(s02,0),1),
        ROUND(100.0*w03/NULLIF(s03,0),1),
        ROUND(100.0*w04/NULLIF(s04,0),1),
        ROUND(100.0*w05/NULLIF(s05,0),1),
        ROUND(100.0*w06/NULLIF(s06,0),1)
    ]) AS win_pct,
    unnest(ARRAY[
        ROUND(100.0*w00/NULLIF(s00,0)-37.5,1),
        ROUND(100.0*w01/NULLIF(s01,0)-37.5,1),
        ROUND(100.0*w02/NULLIF(s02,0)-37.5,1),
        ROUND(100.0*w03/NULLIF(s03,0)-37.5,1),
        ROUND(100.0*w04/NULLIF(s04,0)-37.5,1),
        ROUND(100.0*w05/NULLIF(s05,0)-37.5,1),
        ROUND(100.0*w06/NULLIF(s06,0)-37.5,1)
    ]) AS edge_vs_be
FROM agg;
