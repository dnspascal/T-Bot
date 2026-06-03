package marketstate

import (
	"context"
	"log/slog"

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

// ProcessCandle calculates indicators and stores market state for a new candle.
func (p *Processor) ProcessCandle(ctx context.Context, openTime int64, open, high, low, close float64, volume int64) (indicator.MarketState, error) {
	p.buffer.AddCandle(open, high, low, close, volume)

	marketState := p.calculator.Calculate(
		p.symbolID,
		p.provider,
		p.period,
		openTime,
		open, high, low, close,
		volume,
		p.buffer.Closes(),
		p.buffer.OHLC(),
	)

	if err := p.repo.Insert(ctx, marketState); err != nil {
		slog.Error("failed to store market state", "period", p.period, "symbolID", p.symbolID, "err", err)
		return marketState, err
	}

	return marketState, nil
}

// State returns the most recently calculated MarketState without re-running indicators.
func (p *Processor) State() indicator.MarketState {
	return p.calculator.LastState()
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

// ProcessCandle routes a candle to the matching processor and returns its market state.
func (m *ProcessorManager) ProcessCandle(ctx context.Context, period string, openTime int64, open, high, low, close float64, volume int64) (map[string]indicator.MarketState, error) {
	results := make(map[string]indicator.MarketState)

	processor, ok := m.processors[period]
	if !ok {
		return results, nil
	}

	state, err := processor.ProcessCandle(ctx, openTime, open, high, low, close, volume)
	if err != nil {
		return results, err
	}

	results[period] = state
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
