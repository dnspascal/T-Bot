// Package strategy defines the pluggable trading strategy interface.
//
// Strategies are selected at startup via the STRATEGY environment variable.
// Each strategy is responsible for evaluating market state and returning an
// entry signal. Position monitoring (peak drawback, watcher) is strategy-agnostic
// and lives in the bot package.
//
// Available strategies:
//   - "regime"    — multi-timeframe regime + range bounce (default)
//   - "sr_bounce" — RSI extreme at M15 support/resistance structure
package strategy

import "github.com/denismgaya/t-bot/internal/indicator"

const (
	TierNormal     = 0
	TierStrong     = 1
	TierStronger   = 2
	TierVeryStrong = 3
)

// EntryResult is returned by every strategy's Evaluate call.
type EntryResult struct {
	Signal     string
	Confluence int
	Confidence float64 // 0.0–1.0
	Tier       int
	SLPrice    float64
	TPPrice    float64
	SLPips     float64
	TPPips     float64
	ATR        float64
	Reason     string
}

// Strategy is the core interface every trading strategy must implement.
type Strategy interface {
	// Name returns the short identifier used to tag signals in the database.
	Name() string
	// Evaluate inspects the current market states and price and returns an
	// entry decision. It must never block and must not open or close positions.
	// EOD window checks are handled by the bot before calling Evaluate.
	Evaluate(states map[string]indicator.MarketState, currentPrice, pipSize float64) EntryResult
}

// ConfluenceToTier converts a raw confluence count to a position tier.
func ConfluenceToTier(c int) int {
	switch {
	case c >= 6:
		return TierVeryStrong
	case c >= 5:
		return TierStronger
	case c >= 4:
		return TierStrong
	default:
		return TierNormal
	}
}
