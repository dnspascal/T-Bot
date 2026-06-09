package tick

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

func (r *Repository) Insert(ctx context.Context, t Tick) error {
	const q = `
		INSERT INTO price_ticks
			(symbol_id, bid, ask, session_close, provider_timestamp, received_at, processing_us)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.db.Exec(ctx, q,
		t.SymbolID, t.Bid, t.Ask,
		t.SessionClose, t.ProviderTimestamp,
		t.ReceivedAt, t.ProcessingUS,
	)
	if err != nil {
		return fmt.Errorf("tick.Insert: %w", err)
	}
	return nil
}

func (r *Repository) Recent(ctx context.Context, symbolID string, n int) ([]Tick, error) {
	const q = `
		SELECT id, symbol_id, bid, ask, mid, spread,
		       session_close, provider_timestamp, received_at, processing_us
		FROM price_ticks
		WHERE symbol_id = $1
		ORDER BY received_at DESC
		LIMIT $2`
	rows, err := r.db.Query(ctx, q, symbolID, n)
	if err != nil {
		return nil, fmt.Errorf("tick.Recent: %w", err)
	}
	defer rows.Close()

	var ticks []Tick
	for rows.Next() {
		var t Tick
		if err := rows.Scan(
			&t.ID, &t.SymbolID, &t.Bid, &t.Ask, &t.Mid, &t.Spread,
			&t.SessionClose, &t.ProviderTimestamp, &t.ReceivedAt, &t.ProcessingUS,
		); err != nil {
			return nil, fmt.Errorf("tick.Recent scan: %w", err)
		}
		ticks = append(ticks, t)
	}
	return ticks, nil
}
