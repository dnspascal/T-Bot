package snapshot

import "time"

type Snapshot struct {
	ID              string
	Provider        string
	ProviderAcctID  string
	Balance         float64
	LeverageRatio   *float64
	MaxLeverage     *float64
	AccountMode     *string
	Currency        *string
	BrokerName      *string
	IsLimitedRisk   *bool
	FairStopOut     *bool
	ProviderPayload []byte
	Trigger         *string
	SnapshottedAt   time.Time
}
