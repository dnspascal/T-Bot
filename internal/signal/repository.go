package signal

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Insert(ctx context.Context, s Signal) (string, error) {
	const q = `
		INSERT INTO signals (symbol_id, provider, signal, reason, confluence, confidence, processing_us, checked_market_states, bar_time, strategy)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id`
	strat := s.Strategy
	if strat == "" {
		strat = "regime"
	}
	var id string
	err := r.db.QueryRow(ctx, q,
		s.SymbolID, s.Provider, s.Signal, s.Reason, s.Confluence, s.Confidence, s.ProcessingUS, s.CheckedMarketStates, s.BarTime, strat,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("signal.Insert: %w", err)
	}
	return id, nil
}
