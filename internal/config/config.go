package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// CTraderConfig holds cTrader-specific credentials
type CTraderConfig struct {
	ClientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string
	AccountID    int64
	SymbolID     int64
	Demo         bool
}

// BinanceConfig holds Binance-specific credentials
type BinanceConfig struct {
	APIKey    string
	APISecret string
	TestNet   bool
}

// Config holds all configuration for the trading bot
type Config struct {
	DatabaseURL string

	// Per-provider configurations
	CTrader *CTraderConfig
	Binance *BinanceConfig

	// Provider-specific symbols (for multi-provider mode)
	EnableCTrader  bool
	CTraderSymbol  string
	EnableBinance  bool
	BinanceSymbol  string

	// Risk settings
	RiskPercent    float64
	MaxDailyLoss   float64
	InitialBalance float64 // fallback balance if FetchAccountInfo fails

	// Strategy settings
	StopLossPips   float64
	TakeProfitPips float64
}

func Load() (*Config, error) {
	godotenv.Load()

	// Load cTrader configuration (optional, only if enabled)
	var ctraderCfg *CTraderConfig
	if getEnv("ENABLE_CTRADER", "true") == "true" {
		accountID, err := strconv.ParseInt(mustEnv("CTRADER_ACCOUNT_ID"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("CTRADER_ACCOUNT_ID must be a number: %w", err)
		}

		symbolID, err := strconv.ParseInt(getEnv("CTRADER_SYMBOL_ID", "1"), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("CTRADER_SYMBOL_ID must be a number: %w", err)
		}

		ctraderCfg = &CTraderConfig{
			ClientID:     mustEnv("CTRADER_CLIENT_ID"),
			ClientSecret: mustEnv("CTRADER_CLIENT_SECRET"),
			AccessToken:  mustEnv("CTRADER_ACCESS_TOKEN"),
			RefreshToken: getEnv("CTRADER_REFRESH_TOKEN", ""),
			AccountID:    accountID,
			SymbolID:     symbolID,
			Demo:         getEnv("CTRADER_DEMO", "true") == "true",
		}
	}

	// Load Binance configuration (optional)
	var binanceCfg *BinanceConfig
	if os.Getenv("BINANCE_API_KEY") != "" {
		binanceCfg = &BinanceConfig{
			APIKey:  getEnv("BINANCE_API_KEY", ""),
			APISecret: getEnv("BINANCE_API_SECRET", ""),
			TestNet: getEnv("BINANCE_TESTNET", "true") == "true",
		}
	}

	// Load common settings
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

	cfg := &Config{
		DatabaseURL:    mustEnv("DATABASE_URL"),
		CTrader:        ctraderCfg,
		Binance:        binanceCfg,
		InitialBalance: initialBalance,
		RiskPercent:    riskPercent,
		MaxDailyLoss:   maxDailyLoss,
		StopLossPips:   stopLossPips,
		TakeProfitPips: takeProfitPips,

		// Multi-provider configuration
		EnableCTrader: getEnv("ENABLE_CTRADER", "true") == "true",
		CTraderSymbol: getEnv("CTRADER_SYMBOL", "EURUSD"),
		EnableBinance: getEnv("ENABLE_BINANCE", "false") == "true",
		BinanceSymbol: getEnv("BINANCE_SYMBOL", "BTCUSDT"),
	}

	return cfg, nil
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

