package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string

	// cTrader credentials
	ClientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string
	AccountID    int64
	Demo         bool

	// Risk settings
	RiskPercent    float64
	MaxDailyLoss   float64
	InitialBalance float64 // fallback balance if FetchAccountInfo fails

	// Strategy settings
	Symbol         string
	SymbolID       int64
	StopLossPips   float64
	TakeProfitPips float64
}

func Load() (*Config, error) {
	godotenv.Load()

	accountID, err := strconv.ParseInt(mustEnv("CTRADER_ACCOUNT_ID"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("CTRADER_ACCOUNT_ID must be a number: %w", err)
	}

	symbolID, err := strconv.ParseInt(getEnv("CTRADER_SYMBOL_ID", "1"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("CTRADER_SYMBOL_ID must be a number: %w", err)
	}

	initialBalance, err := strconv.ParseFloat(getEnv("INITIAL_BALANCE", "200.0"), 64)
	if err != nil {
		return nil, fmt.Errorf("INITIAL_BALANCE must be a number: %w", err)
	}

	riskPercent, err := strconv.ParseFloat(getEnv("RISK_PERCENT", "1.0"), 64)
	if err != nil {
		return nil, fmt.Errorf("RISK_PERCENT must be a number: %w", err)
	}

	maxDailyLoss, err := strconv.ParseFloat(getEnv("MAX_DAILY_LOSS", "2.0"), 64)
	if err != nil {
		return nil, fmt.Errorf("MAX_DAILY_LOSS must be a number: %w", err)
	}

	stopLossPips, err := strconv.ParseFloat(getEnv("STOP_LOSS_PIPS", "10.0"), 64)
	if err != nil {
		return nil, fmt.Errorf("STOP_LOSS_PIPS must be a number: %w", err)
	}

	takeProfitPips, err := strconv.ParseFloat(getEnv("TAKE_PROFIT_PIPS", "20.0"), 64)
	if err != nil {
		return nil, fmt.Errorf("TAKE_PROFIT_PIPS must be a number: %w", err)
	}

	return &Config{
		DatabaseURL:    mustEnv("DATABASE_URL"),
		ClientID:       mustEnv("CTRADER_CLIENT_ID"),
		ClientSecret:   mustEnv("CTRADER_CLIENT_SECRET"),
		AccessToken:    mustEnv("CTRADER_ACCESS_TOKEN"),
		RefreshToken:   getEnv("CTRADER_REFRESH_TOKEN", ""),
		AccountID:      accountID,
		Demo:           getEnv("CTRADER_DEMO", "true") == "true",
		InitialBalance: initialBalance,
		RiskPercent:    riskPercent,
		MaxDailyLoss:   maxDailyLoss,
		Symbol:         getEnv("SYMBOL", "EURUSD"),
		SymbolID:       symbolID,
		StopLossPips:   stopLossPips,
		TakeProfitPips: takeProfitPips,
	}, nil
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required env var %s is not set", key))
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (c *Config) Mode() string {
	if c.Demo {
		return "demo"
	}
	return "live"
}
