package fill

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

func (r *Repository) Insert(ctx context.Context, f Fill) error {
	const q = `
		INSERT INTO fills (
			our_order_id, our_position_id, provider, provider_fill_id, provider_order_id, provider_position_id,
			symbol_id, side, volume, filled_volume, execution_price,
			event_type, fill_status, commission, margin_rate, base_to_usd_rate,
			close_entry_price, gross_profit, close_swap, close_commission,
			balance_after, closed_volume, pnl_conversion_fee, trade_duration_ms,
			provider_create_time, provider_exec_time, raw_payload
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,
			$10,$11,$12,$13,$14,$15,$16,$17,$18,
			$19,$20,$21,$22,$23,$24,$25,$26,$27
		) ON CONFLICT (provider, provider_fill_id) DO NOTHING`
	_, err := r.db.Exec(ctx, q,
		f.OurOrderID, f.OurPositionID, f.Provider, f.ProviderFillID, f.ProviderOrderID, f.ProviderPositionID,
		f.SymbolID, f.Side, f.Volume, f.FilledVolume, f.ExecutionPrice,
		f.EventType, f.FillStatus, f.Commission, f.MarginRate, f.BaseToUSDRate,
		f.CloseEntryPrice, f.GrossProfit, f.CloseSwap, f.CloseCommission,
		f.BalanceAfter, f.ClosedVolume, f.PnLConversionFee, f.TradeDurationMs,
		f.ProviderCreateTime, f.ProviderExecTime, f.RawPayload,
	)
	if err != nil {
		return fmt.Errorf("fill.Insert: %w", err)
	}
	return nil
}
