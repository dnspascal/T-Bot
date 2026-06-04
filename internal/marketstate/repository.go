package marketstate

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/denismgaya/t-bot/internal/indicator"
)

// CandleRow represents basic OHLCV data for a candle
type CandleRow struct {
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// Repository defines storage interface for market states
// Easy to swap implementations (PostgreSQL, Mock, etc.)
type Repository interface {
	Insert(ctx context.Context, state indicator.MarketState) error
	Get(ctx context.Context, symbolID, provider, period string, barTime int64) (indicator.MarketState, error)
	GetLatest(ctx context.Context, symbolID, provider, period string) (indicator.MarketState, error)
	GetLastCandles(ctx context.Context, symbol, timeframe string, limit int) ([]CandleRow, error)
}

// PostgresRepository stores market states in PostgreSQL
type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// Insert stores or updates a market state
func (r *PostgresRepository) Insert(ctx context.Context, state indicator.MarketState) error {
	barTime := time.Unix(state.BarTime, 0).UTC()

	_, err := r.db.Exec(ctx, `
		INSERT INTO market_states (
			symbol_id, provider, period, bar_time, processing_ms,
			open, high, low, close, volume,
			ema_fast, ema_slow, rsi, adx, atr,
			support_level, resistance_level, trend_high, trend_low, breakout_level,
			regime, volatility_trend, momentum_direction, volume_ma
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
		ON CONFLICT (symbol_id, provider, period, bar_time)
		DO UPDATE SET
			processing_ms = EXCLUDED.processing_ms,
			ema_fast = EXCLUDED.ema_fast,
			ema_slow = EXCLUDED.ema_slow,
			rsi = EXCLUDED.rsi,
			adx = EXCLUDED.adx,
			atr = EXCLUDED.atr,
			support_level = EXCLUDED.support_level,
			resistance_level = EXCLUDED.resistance_level,
			trend_high = EXCLUDED.trend_high,
			trend_low = EXCLUDED.trend_low,
			breakout_level = EXCLUDED.breakout_level,
			regime = EXCLUDED.regime,
			volatility_trend = EXCLUDED.volatility_trend,
			momentum_direction = EXCLUDED.momentum_direction,
			volume_ma = EXCLUDED.volume_ma
	`, state.SymbolID, state.Provider, state.Period, barTime, state.ProcessingMS,
		state.Open, state.High, state.Low, state.Close, state.Volume,
		state.EMAFast, state.EMASlow, state.RSI, state.ADX, state.ATR,
		state.SupportLevel, state.ResistanceLevel, state.TrendHigh, state.TrendLow, state.BreakoutLevel,
		state.Regime, state.VolatilityTrend, state.MomentumDirection, state.VolumeMA)

	return err
}

// Get retrieves a specific market state
func (r *PostgresRepository) Get(ctx context.Context, symbolID, provider, period string, barTime int64) (indicator.MarketState, error) {
	var state indicator.MarketState

	err := r.db.QueryRow(ctx, `
		SELECT symbol_id, provider, period, bar_time, processing_ms,
		       open, high, low, close, volume,
		       ema_fast, ema_slow, rsi, adx, atr,
		       support_level, resistance_level, trend_high, trend_low, breakout_level,
		       regime, volatility_trend, momentum_direction, volume_ma
		FROM market_states
		WHERE symbol_id = $1 AND provider = $2 AND period = $3 AND bar_time = $4
	`, symbolID, provider, period, barTime).Scan(
		&state.SymbolID, &state.Provider, &state.Period, &state.BarTime, &state.ProcessingMS,
		&state.Open, &state.High, &state.Low, &state.Close, &state.Volume,
		&state.EMAFast, &state.EMASlow, &state.RSI, &state.ADX, &state.ATR,
		&state.SupportLevel, &state.ResistanceLevel, &state.TrendHigh, &state.TrendLow, &state.BreakoutLevel,
		&state.Regime, &state.VolatilityTrend, &state.MomentumDirection, &state.VolumeMA,
	)

	return state, err
}

// GetLatest retrieves the most recent market state for a timeframe
func (r *PostgresRepository) GetLatest(ctx context.Context, symbolID, provider, period string) (indicator.MarketState, error) {
	var state indicator.MarketState

	err := r.db.QueryRow(ctx, `
		SELECT symbol_id, provider, period, bar_time, processing_ms,
		       open, high, low, close, volume,
		       ema_fast, ema_slow, rsi, adx, atr,
		       support_level, resistance_level, trend_high, trend_low, breakout_level,
		       regime, volatility_trend, momentum_direction, volume_ma
		FROM market_states
		WHERE symbol_id = $1 AND provider = $2 AND period = $3
		ORDER BY bar_time DESC
		LIMIT 1
	`, symbolID, provider, period).Scan(
		&state.SymbolID, &state.Provider, &state.Period, &state.BarTime, &state.ProcessingMS,
		&state.Open, &state.High, &state.Low, &state.Close, &state.Volume,
		&state.EMAFast, &state.EMASlow, &state.RSI, &state.ADX, &state.ATR,
		&state.SupportLevel, &state.ResistanceLevel, &state.TrendHigh, &state.TrendLow, &state.BreakoutLevel,
		&state.Regime, &state.VolatilityTrend, &state.MomentumDirection, &state.VolumeMA,
	)

	return state, err
}

// GetLastCandles retrieves the last N candles for a symbol and timeframe
func (r *PostgresRepository) GetLastCandles(ctx context.Context, symbol, timeframe string, limit int) ([]CandleRow, error) {
	query := `
		SELECT open, high, low, close, volume
		FROM market_states
		WHERE symbol = $1 AND timeframe = $2
		ORDER BY bar_time DESC
		LIMIT $3
	`

	rows, err := r.db.Query(ctx, query, symbol, timeframe, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candles []CandleRow
	for rows.Next() {
		var candle CandleRow
		if err := rows.Scan(&candle.Open, &candle.High, &candle.Low, &candle.Close, &candle.Volume); err != nil {
			return nil, err
		}
		candles = append(candles, candle)
	}

	return candles, rows.Err()
}

// MockRepository for testing - no DB needed
type MockRepository struct {
	states map[string]indicator.MarketState
}

func NewMockRepository() *MockRepository {
	return &MockRepository{
		states: make(map[string]indicator.MarketState),
	}
}

func (m *MockRepository) Insert(ctx context.Context, state indicator.MarketState) error {
	key := state.SymbolID + ":" + state.Provider + ":" + state.Period + ":" + string(rune(state.BarTime))
	m.states[key] = state
	return nil
}

func (m *MockRepository) Get(ctx context.Context, symbolID, provider, period string, barTime int64) (indicator.MarketState, error) {
	key := symbolID + ":" + provider + ":" + period + ":" + string(rune(barTime))
	if state, ok := m.states[key]; ok {
		return state, nil
	}
	return indicator.MarketState{}, nil
}

func (m *MockRepository) GetLatest(ctx context.Context, symbolID, provider, period string) (indicator.MarketState, error) {
	var latest indicator.MarketState
	for _, state := range m.states {
		if state.SymbolID == symbolID && state.Provider == provider && state.Period == period {
			if state.BarTime > latest.BarTime {
				latest = state
			}
		}
	}
	return latest, nil
}

func (m *MockRepository) GetLastCandles(ctx context.Context, symbol, timeframe string, limit int) ([]CandleRow, error) {
	// Mock implementation - returns empty slice
	return []CandleRow{}, nil
}
