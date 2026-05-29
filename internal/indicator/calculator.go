package indicator

// MarketState represents calculated indicators for a timeframe
type MarketState struct {
	SymbolID  string
	Provider  string
	Period    string  // M5, M15, M30, H1, H4, D1
	BarTime   int64   // Unix timestamp when bar opened

	// OHLCV
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    int64

	// Indicators
	EMAFast   float64  // EMA(9)
	EMASlow   float64  // EMA(21)
	RSI       float64  // RSI(14)
	ADX       float64  // ADX(14)
	ATR       float64  // ATR(14)

	// Status
	IsWarmedUp bool    // True if all indicators ready (need 21+ candles)
}

// Calculator orchestrates indicator calculation
// Timeframe-agnostic: same calculation for M5, H1, H4, etc.
type Calculator struct {
	emaFastPeriod int  // 9
	emaSlowPeriod int  // 21
	rsiPeriod     int  // 14
	adxPeriod     int  // 14
	atrPeriod     int  // 14
}

// NewCalculator creates a calculator with standard periods
func NewCalculator() *Calculator {
	return &Calculator{
		emaFastPeriod: 9,
		emaSlowPeriod: 21,
		rsiPeriod:     14,
		adxPeriod:     14,
		atrPeriod:     14,
	}
}

// Calculate computes all indicators for a given set of closes and OHLC data
// Input data can be from any timeframe (M5, H1, H4, etc.) - calculation is identical
func (c *Calculator) Calculate(
	symbolID, provider, period string,
	barTime int64,
	open, high, low, close float64,
	volume int64,
	historicalCloses []float64,
	historicalOHLC []OHLC,
) MarketState {
	// Append current candle to historical data for calculation
	allCloses := append(historicalCloses, close)

	ohlcCurrent := OHLC{High: high, Low: low, Close: close}
	allOHLC := append(historicalOHLC, ohlcCurrent)

	ms := MarketState{
		SymbolID:   symbolID,
		Provider:   provider,
		Period:     period,
		BarTime:    barTime,
		Open:       open,
		High:       high,
		Low:        low,
		Close:      close,
		Volume:     volume,
		EMAFast:    CalculateEMA(allCloses, c.emaFastPeriod),
		EMASlow:    CalculateEMA(allCloses, c.emaSlowPeriod),
		RSI:        CalculateRSI(allCloses, c.rsiPeriod),
		ADX:        CalculateADX(allOHLC, c.adxPeriod),
		ATR:        CalculateATR(allOHLC, c.atrPeriod),
	}

	// Indicators are ready when we have at least the longest period (21 for EMA slow)
	ms.IsWarmedUp = len(allCloses) >= c.emaSlowPeriod

	return ms
}

// CalculateFromHistory calculates indicators from historical candles only
// Used for warmup: load 50 historical candles, calculate each point
func (c *Calculator) CalculateFromHistory(
	symbolID, provider, period string,
	barTime int64,
	open, high, low, close float64,
	volume int64,
	historicalCloses []float64,
	historicalOHLC []OHLC,
) MarketState {
	// For history, use the closes/OHLC up to (but not including) this candle
	// This prevents lookahead bias

	ms := MarketState{
		SymbolID:   symbolID,
		Provider:   provider,
		Period:     period,
		BarTime:    barTime,
		Open:       open,
		High:       high,
		Low:        low,
		Close:      close,
		Volume:     volume,
		EMAFast:    CalculateEMA(historicalCloses, c.emaFastPeriod),
		EMASlow:    CalculateEMA(historicalCloses, c.emaSlowPeriod),
		RSI:        CalculateRSI(historicalCloses, c.rsiPeriod),
		ADX:        CalculateADX(historicalOHLC, c.adxPeriod),
		ATR:        CalculateATR(historicalOHLC, c.atrPeriod),
	}

	ms.IsWarmedUp = len(historicalCloses) >= c.emaSlowPeriod

	return ms
}
