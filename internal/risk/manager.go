package risk

import (
	"fmt"
	"time"
)


type Manager struct {
	riskPercent      float64
	maxDailyLossPct  float64 // percent of balance, e.g. 2.0 = 2%

	dailyLoss float64
	dayStart  time.Time

	unitsPerMicroLot    int64
	minVolume           int64
	maxVolume           int64
	pipValuePerMicroLot float64 // USD value of 1 pip on 1 micro lot; forex≈0.10, BTC≈1e-7
}

func New(riskPercent, maxDailyLossPct float64) *Manager {
	return &Manager{
		riskPercent:         riskPercent,
		maxDailyLossPct:     maxDailyLossPct,
		dayStart:            today(),
		unitsPerMicroLot:    1000,
		minVolume:           1000,
		maxVolume:           5_000_000,
		pipValuePerMicroLot: 0.10,
	}
}

// SetVolumeConfig overrides per-provider volume scaling.
// pipValue: USD value of 1 pip on 1 micro lot.
//   CTrader EURUSD: 0.10  (0.0001 × 1000 broker units = $0.10)
//   Binance BTCUSDT: 1e-7 (0.0001 × 100_000 satoshis / 100_000_000 = 1e-7)
func (m *Manager) SetVolumeConfig(unitsPerMicroLot, minVolume, maxVolume int64, pipValue float64) {
	m.unitsPerMicroLot = unitsPerMicroLot
	m.minVolume = minVolume
	m.maxVolume = maxVolume
	m.pipValuePerMicroLot = pipValue
}

var dsmLocation, _ = time.LoadLocation("Africa/Dar_es_Salaam")


func (m *Manager) PositionSize(balance, stopLossPips float64) (int64, error) {
	if stopLossPips < 5 {
		return 0, fmt.Errorf("stop loss too tight: %.1f pips (minimum 5)", stopLossPips)
	}

	riskAmount := balance * (m.riskPercent / 100)

	microLots := riskAmount / (stopLossPips * m.pipValuePerMicroLot)

	volume := int64(microLots * float64(m.unitsPerMicroLot))
	volume = max(m.minVolume, min(volume, m.maxVolume))

	return volume, nil
}

// PositionSizeForTier scales the base position size by the confluence tier multiplier.
// Tier 0 = 1× base, tier 1 = 2× base, tier 2 = 3× base, tier 3 = 4× base.
func (m *Manager) PositionSizeForTier(balance, stopLossPips float64, tier int) (int64, error) {
	base, err := m.PositionSize(balance, stopLossPips)
	if err != nil {
		return 0, err
	}
	return min(base*int64(tier+1), m.maxVolume), nil
}


func (m *Manager) dailyLimit(balance float64) float64 {
	return balance * (m.maxDailyLossPct / 100)
}

func (m *Manager) RecordLoss(amount, balance float64) error {
	m.resetDayIfNeeded()
	m.dailyLoss += amount
	limit := m.dailyLimit(balance)
	if m.dailyLoss >= limit {
		return fmt.Errorf("daily loss limit reached: $%.2f of $%.2f (%.0f%% of balance)",
			m.dailyLoss, limit, m.maxDailyLossPct)
	}
	return nil
}

func (m *Manager) RestoreLoss(amount float64) {
	m.dailyLoss = amount
}

func (m *Manager) CanTrade(balance float64) bool {
	m.resetDayIfNeeded()
	return m.dailyLoss < m.dailyLimit(balance)
}

func (m *Manager) DailyLoss() float64 {
	m.resetDayIfNeeded()
	return m.dailyLoss
}

func (m *Manager) MaxDailyLossPct() float64 {
	return m.maxDailyLossPct
}

func (m *Manager) resetDayIfNeeded() {
	if today().After(m.dayStart) {
		m.dailyLoss = 0
		m.dayStart = today()
	}
}

func today() time.Time {
	now := time.Now().In(dsmLocation)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, dsmLocation)
}
