package order

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Insert(ctx context.Context, o Order) (string, error) {
	const q = `
		INSERT INTO orders (signal_id, provider, symbol, symbol_id, side, volume, sl, tp, sent_at, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending')
		RETURNING id`
	var id string
	err := r.db.QueryRow(ctx, q,
		o.SignalID, o.Provider, o.Symbol, o.SymbolID, o.Side, o.Volume, o.SL, o.TP, o.SentAt,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("order.Insert: %w", err)
	}
	return id, nil
}

func (r *Repository) UpdateExecution(ctx context.Context, id, providerOrderID, providerPositionID string, entryPrice float64, slippagePoints int64, status string, receivedAt time.Time, roundTripMs int64) error {
	const q = `
		UPDATE orders SET
			provider_order_id     = $2,
			provider_position_id  = $3,
			entry_price           = $4,
			slippage_points       = $5,
			status                = $6,
			execution_received_at = $7,
			round_trip_ms         = $8,
			updated_at            = NOW()
		WHERE id = $1`
	_, err := r.db.Exec(ctx, q,
		id, providerOrderID, providerPositionID,
		entryPrice, slippagePoints, status,
		receivedAt, roundTripMs,
	)
	if err != nil {
		return fmt.Errorf("order.UpdateExecution: %w", err)
	}
	return nil
}

func (r *Repository) UpdateError(ctx context.Context, id, errorCode, errorMsg string) error {
	const q = `
		UPDATE orders SET
			status     = 'error',
			error_code = $2,
			error_msg  = $3,
			updated_at = NOW()
		WHERE id = $1`
	_, err := r.db.Exec(ctx, q, id, errorCode, errorMsg)
	if err != nil {
		return fmt.Errorf("order.UpdateError: %w", err)
	}
	return nil
}
