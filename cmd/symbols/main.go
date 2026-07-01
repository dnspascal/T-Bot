package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/denismgaya/t-bot/internal/config"
	"github.com/denismgaya/t-bot/internal/database"
	"github.com/denismgaya/t-bot/internal/provider/ctrader/api"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal("load config:", err)
	}
	if !cfg.EnableCTrader {
		log.Fatal("cTrader not enabled in config")
	}

	ctx := context.Background()
	pool, err := database.New(ctx, cfg.DatabaseURL, 2, 1)
	if err != nil {
		log.Fatal("db connect:", err)
	}
	defer pool.Close()

	accessToken := cfg.CTrader.AccessToken
	var dbToken string
	if err := pool.QueryRow(ctx,
		"SELECT value FROM bot_credentials WHERE key = 'ctrader_access_token'",
	).Scan(&dbToken); err == nil && dbToken != "" {
		accessToken = dbToken
	}

	if accessToken == "" {
		log.Fatal("no cTrader access token found — run the bot first to authenticate")
	}

	client := api.NewClient(cfg.CTrader.Demo, cfg.CTrader.AccountID, cfg.CTrader.SymbolID)
	if err := client.Connect(); err != nil {
		log.Fatal("connect:", err)
	}
	defer client.Close()

	if err := client.AuthApp(cfg.CTrader.ClientID, cfg.CTrader.ClientSecret); err != nil {
		log.Fatal("auth app:", err)
	}
	time.Sleep(2 * time.Second)

	accounts, err := client.GetAccountList(accessToken)
	if err != nil {
		log.Fatal("get account list:", err)
	}

	var ctidAccountID int64
	for _, acc := range accounts {
		if acc.IsLive == !cfg.CTrader.Demo {
			ctidAccountID = acc.CtidTraderAccountID
			fmt.Printf("Account: ctidID=%d login=%d broker=%s\n\n", acc.CtidTraderAccountID, acc.TraderLogin, acc.BrokerName)
			break
		}
	}
	if ctidAccountID == 0 {
		log.Fatal("no matching account found")
	}
	client.SetAccountID(ctidAccountID)

	if err := client.AuthAccount(accessToken); err != nil {
		log.Fatal("auth account:", err)
	}
	time.Sleep(2 * time.Second)

	symbols, err := client.ListSymbols()
	if err != nil {
		log.Fatal("list symbols:", err)
	}

	sort.Slice(symbols, func(i, j int) bool {
		return symbols[i].SymbolName < symbols[j].SymbolName
	})

	fmt.Printf("%-10s %-30s %s\n", "ID", "Name", "Enabled")
	fmt.Println("--------------------------------------------------")
	for _, s := range symbols {
		if s.SymbolName == "" {
			continue
		}
		fmt.Printf("%-10d %-30s %v\n", s.SymbolID, s.SymbolName, s.Enabled)
	}
	fmt.Printf("\nTotal: %d symbols\n", len(symbols))
}
