package event

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Insert(ctx context.Context, eventType string, detail map[string]any, elapsedMs int64) error {
	const q = `INSERT INTO bot_events (event_type, detail, elapsed_ms) VALUES ($1, $2, $3)`
	var detailJSON []byte
	if len(detail) > 0 {
		b, err := json.Marshal(detail)
		if err != nil {
			return fmt.Errorf("event.Insert marshal: %w", err)
		}
		detailJSON = b
	}
	_, err := r.db.Exec(ctx, q, eventType, detailJSON, elapsedMs)
	if err != nil {
		return fmt.Errorf("event.Insert: %w", err)
	}
	return nil
}

func (r *Repository) Recent(ctx context.Context, n int) ([]Event, error) {
	const q = `
		SELECT id, event_type, detail, elapsed_ms, created_at
		FROM bot_events
		ORDER BY created_at DESC
		LIMIT $1`
	rows, err := r.db.Query(ctx, q, n)
	if err != nil {
		return nil, fmt.Errorf("event.Recent: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var detailJSON []byte
		if err := rows.Scan(&e.ID, &e.EventType, &detailJSON, &e.ElapsedMs, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("event.Recent scan: %w", err)
		}
		if len(detailJSON) > 0 {
			if err := json.Unmarshal(detailJSON, &e.Detail); err != nil {
				return nil, fmt.Errorf("event.Recent unmarshal: %w", err)
			}
		}
		events = append(events, e)
	}
	return events, nil
}
