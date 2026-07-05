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
        -- shared condition flags
        -- session: utc_hour 7-21
        -- rsi: BUY 50-60 / SELL 40-50
        -- h1adx: h1_adx > 20
        -- m15gate: NOT (m15 ranging AND h1 doesn't confirm)
        -- vol: volume > volume_ma
        -- sr: near support (BUY) or resistance (SELL)

        -- Full current strategy (all 4 conditions)
        COUNT(*) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
        ) AS s_full,
        SUM(CAST(won AS int)) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
        ) AS w_full,

        -- Drop session filter (keep RSI + H1 ADX + M15 gate)
        COUNT(*) FILTER (WHERE
            ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
        ) AS s_no_session,
        SUM(CAST(won AS int)) FILTER (WHERE
            ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
        ) AS w_no_session,

        -- Drop RSI zone (keep session + H1 ADX + M15 gate)
        COUNT(*) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
        ) AS s_no_rsi,
        SUM(CAST(won AS int)) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
        ) AS w_no_rsi,

        -- Drop H1 ADX filter (keep session + RSI + M15 gate)
        COUNT(*) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
        ) AS s_no_h1adx,
        SUM(CAST(won AS int)) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
        ) AS w_no_h1adx,

        -- Drop M15 gate (keep session + RSI + H1 ADX)
        COUNT(*) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
        ) AS s_no_m15gate,
        SUM(CAST(won AS int)) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
        ) AS w_no_m15gate,

        -- Current strategy + volume confirmation
        COUNT(*) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
            AND m5_vol > 0 AND m5_vol_ma > 0 AND m5_vol > m5_vol_ma
        ) AS s_plus_vol,
        SUM(CAST(won AS int)) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
            AND m5_vol > 0 AND m5_vol_ma > 0 AND m5_vol > m5_vol_ma
        ) AS w_plus_vol,

        -- Current strategy + near S/R
        COUNT(*) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
            AND ((direction='BUY' AND support_level>0 AND (entry_price-support_level)<=1.5*m5_atr) OR (direction='SELL' AND resistance_level>0 AND (resistance_level-entry_price)<=1.5*m5_atr))
        ) AS s_plus_sr,
        SUM(CAST(won AS int)) FILTER (WHERE
            utc_hour BETWEEN 7 AND 21
            AND ((direction='BUY' AND m5_rsi>50 AND m5_rsi<60) OR (direction='SELL' AND m5_rsi<50 AND m5_rsi>40))
            AND h1_adx > 20
            AND NOT (m15_regime='ranging' AND (h1_regime IS NULL OR h1_regime='ranging' OR (direction='BUY' AND h1_regime<>'trending_up') OR (direction='SELL' AND h1_regime<>'trending_down')))
            AND ((direction='BUY' AND support_level>0 AND (entry_price-support_level)<=1.5*m5_atr) OR (direction='SELL' AND resistance_level>0 AND (resistance_level-entry_price)<=1.5*m5_atr))
        ) AS w_plus_sr
    FROM with_htf
)
SELECT
    unnest(ARRAY[
        '00. Current strategy (all conditions)',
        '01. DROP session filter',
        '02. DROP RSI zone',
        '03. DROP H1 ADX>20',
        '04. DROP M15 gate',
        '05. ADD volume>MA',
        '06. ADD near S/R'
    ]) AS condition,
    unnest(ARRAY[s_full,s_no_session,s_no_rsi,s_no_h1adx,s_no_m15gate,s_plus_vol,s_plus_sr]) AS setups,
    unnest(ARRAY[w_full,w_no_session,w_no_rsi,w_no_h1adx,w_no_m15gate,w_plus_vol,w_plus_sr]) AS wins,
    unnest(ARRAY[
        ROUND(100.0*w_full/NULLIF(s_full,0),1),
        ROUND(100.0*w_no_session/NULLIF(s_no_session,0),1),
        ROUND(100.0*w_no_rsi/NULLIF(s_no_rsi,0),1),
        ROUND(100.0*w_no_h1adx/NULLIF(s_no_h1adx,0),1),
        ROUND(100.0*w_no_m15gate/NULLIF(s_no_m15gate,0),1),
        ROUND(100.0*w_plus_vol/NULLIF(s_plus_vol,0),1),
        ROUND(100.0*w_plus_sr/NULLIF(s_plus_sr,0),1)
    ]) AS win_pct
FROM agg;
