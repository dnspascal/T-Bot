package symbol

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SymbolLookup struct {
	uuids map[string]string
}

func (s *SymbolLookup) Get(name string) (string, error) {
	uuid, ok := s.uuids[name]
	if !ok {
		return "", fmt.Errorf("symbol not found: %s", name)
	}
	return uuid, nil
}

func LoadLookup(ctx context.Context, pool *pgxpool.Pool, symbolNames []string) (*SymbolLookup, error) {
	rows, err := pool.Query(ctx,
		"SELECT symbol, id FROM symbols WHERE symbol = ANY($1)",
		symbolNames,
	)
	if err != nil {
		return nil, fmt.Errorf("query symbols: %w", err)
	}
	defer rows.Close()

	uuids := make(map[string]string)
	for rows.Next() {
		var symbol, uuid string
		if err := rows.Scan(&symbol, &uuid); err != nil {
			return nil, fmt.Errorf("scan symbol: %w", err)
		}
		uuids[symbol] = uuid
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate symbols: %w", err)
	}

	if len(uuids) == 0 {
		return nil, fmt.Errorf("no symbols found in database")
	}

	return &SymbolLookup{uuids: uuids}, nil
}