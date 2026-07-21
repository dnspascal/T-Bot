package combined

import (
	"github.com/denismgaya/t-bot/internal/indicator"
	"github.com/denismgaya/t-bot/internal/strategy"
)

type CombinedStrategy struct {
	strategies []strategy.Strategy
}

func New(strategies ...strategy.Strategy) *CombinedStrategy {
	return &CombinedStrategy{strategies: strategies}
}
func (c *CombinedStrategy) Name() string { return "combined" }

func (c *CombinedStrategy) Evaluate(states map[string]indicator.MarketState, currentPrice, pipSize float64) strategy.EntryResult {

	for _, s := range c.strategies {
		result := s.Evaluate(states, currentPrice, pipSize)
		if result.Signal != strategy.SignalHold {
			result.StrategyName = s.Name()
			return result
		}
	}

	return strategy.EntryResult{Signal: strategy.SignalHold, Reason: "no strategy signaled a trade"}
}
