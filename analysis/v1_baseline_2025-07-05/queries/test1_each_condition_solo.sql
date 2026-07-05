-- Test 1: Each condition tested in isolation against all base setups.
-- For each condition, we take ALL M5 trending/breakout setups and apply
-- only that one condition. Shows which conditions select better or worse
-- setups from the full pool of 1,361 possible entries.
--
-- "won" = price moved >= 1 ATR in signal direction within 60 minutes.
-- Breakeven win rate at 1.67 R:R = 37.5%
--
-- Run: ssh makini "psql 'DB_URL'" < test1_each_condition_solo.sql

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
        h1.regime  AS h1_regime,
        h1.adx     AS h1_adx
    FROM with_outcome o
    LEFT JOIN LATERAL (SELECT regime FROM market_states WHERE symbol_id = o.symbol_id AND period = 'M15' AND bar_time <= o.bar_time ORDER BY bar_time DESC LIMIT 1) m15 ON true
    LEFT JOIN LATERAL (SELECT regime FROM market_states WHERE symbol_id = o.symbol_id AND period = 'M30' AND bar_time <= o.bar_time ORDER BY bar_time DESC LIMIT 1) m30 ON true
    LEFT JOIN LATERAL (SELECT regime, adx FROM market_states WHERE symbol_id = o.symbol_id AND period = 'H1' AND bar_time <= o.bar_time ORDER BY bar_time DESC LIMIT 1) h1 ON true
),
agg AS (
    SELECT
        COUNT(*) AS s00, SUM(CAST(won AS int)) AS w00,
        COUNT(*) FILTER (WHERE utc_hour BETWEEN 7 AND 21) AS s01, SUM(CAST(won AS int)) FILTER (WHERE utc_hour BETWEEN 7 AND 21) AS w01,
        COUNT(*) FILTER (WHERE (direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40)) AS s02, SUM(CAST(won AS int)) FILTER (WHERE (direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40)) AS w02,
        COUNT(*) FILTER (WHERE m5_adx > 20) AS s03, SUM(CAST(won AS int)) FILTER (WHERE m5_adx > 20) AS w03,
        COUNT(*) FILTER (WHERE h1_adx > 20) AS s04, SUM(CAST(won AS int)) FILTER (WHERE h1_adx > 20) AS w04,
        COUNT(*) FILTER (WHERE m15_regime = 'ranging') AS s05, SUM(CAST(won AS int)) FILTER (WHERE m15_regime = 'ranging') AS w05,
        COUNT(*) FILTER (WHERE (direction='BUY' AND m15_regime='trending_up') OR (direction='SELL' AND m15_regime='trending_down')) AS s06, SUM(CAST(won AS int)) FILTER (WHERE (direction='BUY' AND m15_regime='trending_up') OR (direction='SELL' AND m15_regime='trending_down')) AS w06,
        COUNT(*) FILTER (WHERE (direction='BUY' AND h1_regime='trending_up') OR (direction='SELL' AND h1_regime='trending_down')) AS s07, SUM(CAST(won AS int)) FILTER (WHERE (direction='BUY' AND h1_regime='trending_up') OR (direction='SELL' AND h1_regime='trending_down')) AS w07,
        COUNT(*) FILTER (WHERE (direction='BUY' AND h1_regime='trending_down') OR (direction='SELL' AND h1_regime='trending_up')) AS s08, SUM(CAST(won AS int)) FILTER (WHERE (direction='BUY' AND h1_regime='trending_down') OR (direction='SELL' AND h1_regime='trending_up')) AS w08,
        COUNT(*) FILTER (WHERE m5_vol > 0 AND m5_vol_ma > 0 AND m5_vol > m5_vol_ma) AS s09, SUM(CAST(won AS int)) FILTER (WHERE m5_vol > 0 AND m5_vol_ma > 0 AND m5_vol > m5_vol_ma) AS w09,
        COUNT(*) FILTER (WHERE (direction='BUY' AND support_level>0 AND (entry_price-support_level)<=1.5*m5_atr) OR (direction='SELL' AND resistance_level>0 AND (resistance_level-entry_price)<=1.5*m5_atr)) AS s10, SUM(CAST(won AS int)) FILTER (WHERE (direction='BUY' AND support_level>0 AND (entry_price-support_level)<=1.5*m5_atr) OR (direction='SELL' AND resistance_level>0 AND (resistance_level-entry_price)<=1.5*m5_atr)) AS w10,
        COUNT(*) FILTER (WHERE utc_hour BETWEEN 7 AND 21 AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40)) AND h1_adx>20 AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))) AS s11, SUM(CAST(won AS int)) FILTER (WHERE utc_hour BETWEEN 7 AND 21 AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40)) AND h1_adx>20 AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))) AS w11
    FROM with_htf
)
SELECT
    unnest(ARRAY['00. Base signal (no filters)','01. Session filter only','02. RSI zone only','03. M5 ADX>20 only','04. H1 ADX>20 only','05. M15 ranging only','06. M15 same direction only','07. H1 same direction only','08. H1 opposite direction only','09. Volume above MA only','10. Near S/R only','11. Current strategy (all combined)']) AS condition,
    unnest(ARRAY[s00,s01,s02,s03,s04,s05,s06,s07,s08,s09,s10,s11]) AS setups,
    unnest(ARRAY[w00,w01,w02,w03,w04,w05,w06,w07,w08,w09,w10,w11]) AS wins,
    unnest(ARRAY[
        ROUND(100.0*w00/NULLIF(s00,0),1),
        ROUND(100.0*w01/NULLIF(s01,0),1),
        ROUND(100.0*w02/NULLIF(s02,0),1),
        ROUND(100.0*w03/NULLIF(s03,0),1),
        ROUND(100.0*w04/NULLIF(s04,0),1),
        ROUND(100.0*w05/NULLIF(s05,0),1),
        ROUND(100.0*w06/NULLIF(s06,0),1),
        ROUND(100.0*w07/NULLIF(s07,0),1),
        ROUND(100.0*w08/NULLIF(s08,0),1),
        ROUND(100.0*w09/NULLIF(s09,0),1),
        ROUND(100.0*w10/NULLIF(s10,0),1),
        ROUND(100.0*w11/NULLIF(s11,0),1)
    ]) AS win_pct
FROM agg;
