package ctrader

import (
	"log/slog"

	"github.com/denismgaya/t-bot/internal/api"
)

// Setup subscribes to cTrader market data streams
func (c *CTrader) Setup() error {
	if c.client == nil {
		return nil
	}

	// Subscribe to spot price updates
	if err := c.client.SubscribeSpots(); err != nil {
		return err
	}

	// Subscribe to all trading timeframes
	tradingPeriods := []struct {
		code   uint32
		name   string
	}{
		{api.PeriodM5, "M5"},
		{api.PeriodM15, "M15"},
		{api.PeriodM30, "M30"},
		{api.PeriodH1, "H1"},
		{api.PeriodH4, "H4"},
		{api.PeriodD1, "D1"},
	}

	var periodNames []string
	for _, period := range tradingPeriods {
		if err := c.client.SubscribeLiveTrendbar(period.code); err != nil {
			return err
		}
		periodNames = append(periodNames, period.name)
	}
	slog.Info("subscribed to live trendbar", "periods", periodNames)

	return nil
}
