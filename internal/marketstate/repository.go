package marketstate

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/denismgaya/t-bot/internal/indicator"
)

// Repository defines storage interface for market states
// Easy to swap implementations (PostgreSQL, Mock, etc.)
type Repository interface {
	Insert(ctx context.Context, state indicator.MarketState) error
	Get(ctx context.Context, symbolID, provider, period string, barTime int64) (indicator.MarketState, error)
	GetLatest(ctx context.Context, symbolID, provider, period string) (indicator.MarketState, error)
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
	// Convert Unix timestamp (milliseconds) to time.Time
	barTime := time.UnixMilli(state.BarTime)

	_, err := r.db.Exec(ctx, `
		INSERT INTO market_states (
			symbol_id, provider, period, bar_time,
			open, high, low, close, volume,
			ema_fast, ema_slow, rsi, adx, atr
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (symbol_id, provider, period, bar_time)
		DO UPDATE SET
			ema_fast = EXCLUDED.ema_fast,
			ema_slow = EXCLUDED.ema_slow,
			rsi = EXCLUDED.rsi,
			adx = EXCLUDED.adx,
			atr = EXCLUDED.atr
	`, state.SymbolID, state.Provider, state.Period, barTime,
		state.Open, state.High, state.Low, state.Close, state.Volume,
		state.EMAFast, state.EMASlow, state.RSI, state.ADX, state.ATR)

	return err
}

// Get retrieves a specific market state
func (r *PostgresRepository) Get(ctx context.Context, symbolID, provider, period string, barTime int64) (indicator.MarketState, error) {
	var state indicator.MarketState

	err := r.db.QueryRow(ctx, `
		SELECT symbol_id, provider, period, bar_time,
		       open, high, low, close, volume,
		       ema_fast, ema_slow, rsi, adx, atr
		FROM market_states
		WHERE symbol_id = $1 AND provider = $2 AND period = $3 AND bar_time = $4
	`, symbolID, provider, period, barTime).Scan(
		&state.SymbolID, &state.Provider, &state.Period, &state.BarTime,
		&state.Open, &state.High, &state.Low, &state.Close, &state.Volume,
		&state.EMAFast, &state.EMASlow, &state.RSI, &state.ADX, &state.ATR,
	)

	return state, err
}

// GetLatest retrieves the most recent market state for a timeframe
func (r *PostgresRepository) GetLatest(ctx context.Context, symbolID, provider, period string) (indicator.MarketState, error) {
	var state indicator.MarketState

	err := r.db.QueryRow(ctx, `
		SELECT symbol_id, provider, period, bar_time,
		       open, high, low, close, volume,
		       ema_fast, ema_slow, rsi, adx, atr
		FROM market_states
		WHERE symbol_id = $1 AND provider = $2 AND period = $3
		ORDER BY bar_time DESC
		LIMIT 1
	`, symbolID, provider, period).Scan(
		&state.SymbolID, &state.Provider, &state.Period, &state.BarTime,
		&state.Open, &state.High, &state.Low, &state.Close, &state.Volume,
		&state.EMAFast, &state.EMASlow, &state.RSI, &state.ADX, &state.ATR,
	)

	return state, err
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
