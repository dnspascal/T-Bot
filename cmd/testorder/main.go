package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/denismgaya/t-bot/internal/provider/binance"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	apiKey := os.Getenv("BINANCE_API_KEY")
	apiSecret := os.Getenv("BINANCE_API_SECRET")
	testnet := os.Getenv("BINANCE_TESTNET") == "true"

	if testnet && os.Getenv("BINANCE_TESTNET_API_KEY") != "" {
		apiKey = os.Getenv("BINANCE_TESTNET_API_KEY")
		apiSecret = os.Getenv("BINANCE_TESTNET_API_SECRET")
	}

	if apiKey == "" {
		log.Fatal("BINANCE_API_KEY not set")
	}

	symbol := "BTCUSDT"
	qty := 0.001 // minimum lot size for BTCUSDT futures

	client := binance.NewRestClient(apiKey, apiSecret, testnet)

	fmt.Printf("testnet=%v symbol=%s qty=%.3f\n", testnet, symbol, qty)
	fmt.Println("placing BUY...")

	buyID, err := client.PlaceMarketOrder(symbol, "BUY", qty)
	if err != nil {
		log.Fatalf("BUY failed: %v", err)
	}
	fmt.Printf("BUY placed: orderID=%s\n", buyID)

	time.Sleep(2 * time.Second)

	fmt.Println("closing position with SELL reduce-only...")
	sellID, err := client.PlaceReduceOnlyOrder(symbol, "SELL", qty)
	if err != nil {
		log.Fatalf("SELL reduce-only failed: %v", err)
	}
	fmt.Printf("SELL placed: orderID=%s\n", sellID)

	fmt.Println("done — round trip complete")
}
