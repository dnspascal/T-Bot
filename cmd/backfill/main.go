package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"

	"github.com/denismgaya/t-bot/internal/bot"
	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/provider/ctrader/api"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SymbolStruct struct {
	name string
	id   int64
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	db, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal("pgxpool.New", err)
	}
	defer db.Close()

	token, err := bot.LoadCredential(ctx, db, "ctrader_access_token")
	if err != nil {
		log.Fatal("Loading ctrader_access_token: " + err.Error())
	}

	symbols := []SymbolStruct{
		{name: "EURUSD", id: 1},
		{name: "XAUUSD", id: 41},
	}

	periods := []uint32{api.PeriodM5, api.PeriodM15, api.PeriodH1}

	for _, s := range symbols {

		conn := api.NewClient(false, cfg.CTrader.AccountID, s.id, 100000, 0.00001)
		if err := conn.Connect(); err != nil {
			log.Fatal("Connect", err)
		}

		if err := conn.AuthApp(cfg.CTrader.ClientID, cfg.CTrader.ClientSecret); err != nil {
			log.Fatal("AuthApp", err)
		}

		if err := conn.AuthAccount(token); err != nil {
			log.Fatal("AuthAccount", err)
		}

		f, err := os.Create(s.name + ".csv")
		if err != nil {
			log.Fatal("Create CSV file", err)
		}
		w := csv.NewWriter(f)
		w.Write([]string{"symbol", "period", "open_time", "open", "high", "low", "close", "volume"})

		for _, p := range periods {
			log.Printf("Backfilling %s %s", s.name, api.PeriodToString(p))
			bars, err := conn.FetchHistoricalTrendbars(p, 6000)
			if err != nil {
				log.Fatal("FetchHistoricalTrendbars", err)
			}

			log.Printf("Fetched %d bars", len(bars))

			for _, bar := range bars {
				w.Write([]string{
					s.name,
					api.PeriodToString(p),
					fmt.Sprintf("%d", bar.OpenTime),
					fmt.Sprintf("%f", bar.Open),
					fmt.Sprintf("%f", bar.High),
					fmt.Sprintf("%f", bar.Low),
					fmt.Sprintf("%f", bar.Close),
					fmt.Sprintf("%d", bar.Volume),
				})
			}
		}
		w.Flush()
		f.Close()
		conn.Close()
	}

}
