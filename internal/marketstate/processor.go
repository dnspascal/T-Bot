package marketstate

import (
	"context"
	"log/slog"

	"github.com/denismgaya/t-bot/internal/api"
	"github.com/denismgaya/t-bot/internal/indicator"
)

// Processor calculates and stores market states for live candles
// One instance per timeframe
type Processor struct {
	symbolID   string
	provider   string
	period     string
	buffer     CandleBuffer
	calculator *indicator.Calculator
	repo       Repository
}

func NewProcessor(
	symbolID, provider, period string,
	buffer CandleBuffer,
	repo Repository,
) *Processor {
	return &Processor{
		symbolID:   symbolID,
		provider:   provider,
		period:     period,
		buffer:     buffer,
		calculator: indicator.NewCalculator(),
		repo:       repo,
	}
}

// ProcessCandle calculates indicators and stores market state for a new candle
// Returns the calculated market state
func (p *Processor) ProcessCandle(ctx context.Context, bar api.Trendbar) (indicator.MarketState, error) {
	// Convert period code to string
	periodStr := api.PeriodToString(bar.Period)

	// Add candle to buffer (maintains sliding window)
	p.buffer.AddCandle(bar.Open, bar.High, bar.Low, bar.Close, bar.Volume)

	// Calculate all indicators
	marketState := p.calculator.Calculate(
		p.symbolID,
		p.provider,
		periodStr,
		bar.OpenTime,
		bar.Open,
		bar.High,
		bar.Low,
		bar.Close,
		bar.Volume,
		p.buffer.Closes(),
		p.buffer.OHLC(),
	)

	// Store in database
	if err := p.repo.Insert(ctx, marketState); err != nil {
		slog.Error("failed to store market state",
			"period", periodStr,
			"symbolID", p.symbolID,
			"err", err,
		)
		return marketState, err
	}

	return marketState, nil
}

// State returns the most recent calculated market state (from memory)
// This is fast (no DB query) for use in signal generation
func (p *Processor) State() indicator.MarketState {
	closes := p.buffer.Closes()
	ohlc := p.buffer.OHLC()

	if len(closes) == 0 {
		return indicator.MarketState{
			SymbolID: p.symbolID,
			Provider: p.provider,
			Period:   p.period,
		}
	}

	// Calculate from current buffer
	return p.calculator.Calculate(
		p.symbolID,
		p.provider,
		p.period,
		0,  // barTime not used here
		0,  // open not used
		0,  // high not used
		0,  // low not used
		closes[len(closes)-1],  // current close
		0,  // volume not used
		closes,
		ohlc,
	)
}

// IsWarmedUp returns true if this processor has enough data for indicators
func (p *Processor) IsWarmedUp() bool {
	return p.buffer.IsWarmedUp()
}

// ProcessorManager manages market state processors for all timeframes
type ProcessorManager struct {
	processors map[string]*Processor  // key: period (M5, H1, etc.)
	symbolID   string
	provider   string
	repo       Repository
}

func NewProcessorManager(symbolID, provider string, repo Repository) *ProcessorManager {
	return &ProcessorManager{
		processors: make(map[string]*Processor),
		symbolID:   symbolID,
		provider:   provider,
		repo:       repo,
	}
}

// AddProcessor registers a processor for a timeframe
func (m *ProcessorManager) AddProcessor(period string, processor *Processor) {
	m.processors[period] = processor
}

// ProcessCandle processes a candle across all registered processors
// Returns map of period -> market state
func (m *ProcessorManager) ProcessCandle(ctx context.Context, bar api.Trendbar) (map[string]indicator.MarketState, error) {
	periodStr := api.PeriodToString(bar.Period)

	results := make(map[string]indicator.MarketState)

	processor, ok := m.processors[periodStr]
	if !ok {
		slog.Warn("no processor for period", "period", periodStr)
		return results, nil
	}

	state, err := processor.ProcessCandle(ctx, bar)
	if err != nil {
		return results, err
	}

	results[periodStr] = state
	return results, nil
}

// GetAllStates returns current market states for all timeframes
func (m *ProcessorManager) GetAllStates() map[string]indicator.MarketState {
	states := make(map[string]indicator.MarketState)
	for period, processor := range m.processors {
		states[period] = processor.State()
	}
	return states
}

// AllWarmedUp returns true if all processors have enough data
func (m *ProcessorManager) AllWarmedUp() bool {
	for _, processor := range m.processors {
		if !processor.IsWarmedUp() {
			return false
		}
	}
	return len(m.processors) > 0
}
