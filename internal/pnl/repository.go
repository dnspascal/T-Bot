package pnl

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

func (r *Repository) Upsert(ctx context.Context, symbol string, realized, grossProfit, commission, swap float64, isWin bool, roundTripMs, slippagePoints int64) error {
	const q = `
		INSERT INTO daily_pnl
			(date, symbol, realized_pnl, gross_profit, total_commission, total_swap,
			 trade_count, win_count, loss_count, avg_round_trip_ms, avg_slippage_points, updated_at)
		VALUES (CURRENT_DATE, $1, $2, $3, $4, $5, 1, $6, $7, $8, $9, NOW())
		ON CONFLICT (date, symbol) DO UPDATE SET
			realized_pnl        = daily_pnl.realized_pnl    + EXCLUDED.realized_pnl,
			gross_profit        = daily_pnl.gross_profit     + EXCLUDED.gross_profit,
			total_commission    = daily_pnl.total_commission + EXCLUDED.total_commission,
			total_swap          = daily_pnl.total_swap       + EXCLUDED.total_swap,
			trade_count         = daily_pnl.trade_count      + 1,
			win_count           = daily_pnl.win_count        + EXCLUDED.win_count,
			loss_count          = daily_pnl.loss_count       + EXCLUDED.loss_count,
			avg_round_trip_ms   = (daily_pnl.avg_round_trip_ms   * daily_pnl.trade_count + EXCLUDED.avg_round_trip_ms)
			                      / (daily_pnl.trade_count + 1),
			avg_slippage_points = (daily_pnl.avg_slippage_points * daily_pnl.trade_count + EXCLUDED.avg_slippage_points)
			                      / (daily_pnl.trade_count + 1),
			updated_at          = NOW()`
	winCount, lossCount := 0, 0
	if isWin {
		winCount = 1
	} else {
		lossCount = 1
	}
	_, err := r.db.Exec(ctx, q, symbol, realized, grossProfit, commission, swap, winCount, lossCount, roundTripMs, slippagePoints)
	if err != nil {
		return fmt.Errorf("pnl.Upsert: %w", err)
	}
	return nil
}

func (r *Repository) Today(ctx context.Context, symbol string) (float64, error) {
	const q = `SELECT COALESCE(realized_pnl, 0) FROM daily_pnl WHERE date = CURRENT_DATE AND symbol = $1`
	var v float64
	if err := r.db.QueryRow(ctx, q, symbol).Scan(&v); err != nil {
		return 0, nil
	}
	return v, nil
}

func (r *Repository) All(ctx context.Context) ([]DailyPnL, error) {
	const q = `
		SELECT id, date, symbol, realized_pnl, gross_profit, total_commission, total_swap,
		       trade_count, win_count, loss_count, avg_round_trip_ms, avg_slippage_points, updated_at
		FROM daily_pnl
		ORDER BY date DESC`
	rows, err := r.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("pnl.All: %w", err)
	}
	defer rows.Close()

	var records []DailyPnL
	for rows.Next() {
		var d DailyPnL
		if err := rows.Scan(
			&d.ID, &d.Date, &d.Symbol, &d.RealizedPnL, &d.GrossProfit, &d.TotalCommission, &d.TotalSwap,
			&d.TradeCount, &d.WinCount, &d.LossCount, &d.AvgRoundTripMs, &d.AvgSlippagePoints, &d.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("pnl.All scan: %w", err)
		}
		records = append(records, d)
	}
	return records, nil
}
