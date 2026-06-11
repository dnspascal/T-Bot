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
		INSERT INTO signals (symbol_id, provider, signal, confluence, confidence, processing_us, checked_market_states, bar_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`
	var id string
	err := r.db.QueryRow(ctx, q,
		s.SymbolID, s.Provider, s.Signal, s.Confluence, s.Confidence, s.ProcessingUS, s.CheckedMarketStates, s.BarTime,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("signal.Insert: %w", err)
	}
	return id, nil
}
