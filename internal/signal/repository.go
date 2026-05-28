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
		INSERT INTO signals (symbol_id, signal, fast_ema, slow_ema, rsi, confluence, price_mid, processing_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id`
	var id string
	err := r.db.QueryRow(ctx, q,
		s.SymbolID, s.Signal, s.FastEMA, s.SlowEMA, s.RSI, s.Confluence, s.PriceMid, s.ProcessingMs,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("signal.Insert: %w", err)
	}
	return id, nil
}
