package config

var TradingPeriods = []string{"M5", "M15", "M30", "H1", "H4", "D1"}

// PeriodToBinanceInterval maps trading periods to Binance API interval format
var PeriodToBinanceInterval = map[string]string{
	"M5":  "5m",
	"M15": "15m",
	"M30": "30m",
	"H1":  "1h",
	"H4":  "4h",
	"D1":  "1d",
}

// BinanceIntervals returns all Binance interval formats for subscribing to websocket streams
func BinanceIntervals() []string {
	intervals := make([]string, len(TradingPeriods))
	for i, period := range TradingPeriods {
		intervals[i] = PeriodToBinanceInterval[period]
	}
	return intervals
}
