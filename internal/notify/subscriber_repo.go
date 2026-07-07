package notify

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SubscriberRepo struct {
	db *pgxpool.Pool
}

func NewSubscriberRepo(db *pgxpool.Pool) *SubscriberRepo {
	return &SubscriberRepo{db: db}
}

// Upsert inserts or updates a subscriber by their Telegram chat ID.
// The chat ID is stored as the recipient_id in subscriber_targets.
func (r *SubscriberRepo) Upsert(ctx context.Context, chatID, name string) error {
	const upsertSub = `
		INSERT INTO subscribers (name, status)
		VALUES ($1, 'active')
		ON CONFLICT DO NOTHING
		RETURNING id`

	// Check if a target already exists for this chat ID
	const findTarget = `SELECT subscriber_id FROM subscriber_targets WHERE channel = 'telegram' AND recipient_id = $1`
	var existingSubID string
	_ = r.db.QueryRow(ctx, findTarget, chatID).Scan(&existingSubID)
	if existingSubID != "" {
		// already subscribed — update name if we have one
		if name != "" {
			r.db.Exec(ctx, `UPDATE subscribers SET name = $1 WHERE id = $2`, name, existingSubID)
		}
		return nil
	}

	// Create new subscriber + target in a transaction
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("notify: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var subID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO subscribers (name, status) VALUES ($1, 'active') RETURNING id`, name,
	).Scan(&subID); err != nil {
		return fmt.Errorf("notify: insert subscriber: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO subscriber_targets (subscriber_id, channel, recipient_id, status)
		VALUES ($1, 'telegram', $2, 'active')`, subID, chatID,
	); err != nil {
		return fmt.Errorf("notify: insert subscriber_target: %w", err)
	}

	return tx.Commit(ctx)
}
