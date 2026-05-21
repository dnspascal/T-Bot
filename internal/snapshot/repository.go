package snapshot

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

func (r *Repository) Insert(ctx context.Context, s Snapshot) error {
	const q = `
		INSERT INTO account_snapshots
			(provider, provider_acct_id, balance, leverage_ratio, max_leverage,
			 account_mode, currency, broker_name, is_limited_risk, fair_stop_out,
			 provider_payload, trigger, snapshotted_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`
	_, err := r.db.Exec(ctx, q,
		s.Provider, s.ProviderAcctID, s.Balance, s.LeverageRatio, s.MaxLeverage,
		s.AccountMode, s.Currency, s.BrokerName, s.IsLimitedRisk, s.FairStopOut,
		s.ProviderPayload, s.Trigger, s.SnapshottedAt,
	)
	if err != nil {
		return fmt.Errorf("snapshot.Insert: %w", err)
	}
	return nil
}

func (r *Repository) Latest(ctx context.Context, provider, providerAcctID string) (*Snapshot, error) {
	const q = `
		SELECT id, provider, provider_acct_id, balance, leverage_ratio, max_leverage,
		       account_mode, currency, broker_name, is_limited_risk, fair_stop_out,
		       provider_payload, snapshotted_at
		FROM account_snapshots
		WHERE provider = $1 AND provider_acct_id = $2
		ORDER BY snapshotted_at DESC
		LIMIT 1`
	var s Snapshot
	err := r.db.QueryRow(ctx, q, provider, providerAcctID).Scan(
		&s.ID, &s.Provider, &s.ProviderAcctID, &s.Balance, &s.LeverageRatio, &s.MaxLeverage,
		&s.AccountMode, &s.Currency, &s.BrokerName, &s.IsLimitedRisk, &s.FairStopOut,
		&s.ProviderPayload, &s.SnapshottedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("snapshot.Latest: %w", err)
	}
	return &s, nil
}
