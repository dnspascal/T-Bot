package risk

import (
	"fmt"
	"time"
)


type Manager struct {
	riskPercent  float64
	maxDailyLoss float64

	dailyLoss float64
	dayStart  time.Time

	unitsPerMicroLot int64
	minVolume        int64
	maxVolume        int64
}

func New(riskPercent, maxDailyLoss float64) *Manager {
	return &Manager{
		riskPercent:      riskPercent,
		maxDailyLoss:     maxDailyLoss,
		dayStart:         today(),
		unitsPerMicroLot: 1000,       // CTrader default: 1 micro lot = 1,000 broker units
		minVolume:        1000,        // minimum 1 micro lot
		maxVolume:        5_000_000,
	}
}

// SetVolumeConfig overrides the per-provider volume scaling.
// Binance: unitsPerMicroLot=100_000 (satoshi-scale), CTrader: 1_000.
func (m *Manager) SetVolumeConfig(unitsPerMicroLot, minVolume, maxVolume int64) {
	m.unitsPerMicroLot = unitsPerMicroLot
	m.minVolume = minVolume
	m.maxVolume = maxVolume
}

var dsmLocation, _ = time.LoadLocation("Africa/Dar_es_Salaam")


func (m *Manager) PositionSize(balance, stopLossPips float64) (int64, error) {
	if stopLossPips < 5 {
		return 0, fmt.Errorf("stop loss too tight: %.1f pips (minimum 5)", stopLossPips)
	}

	riskAmount := balance * (m.riskPercent / 100)

	pipValuePerMicroLot := 0.10
	microLots := riskAmount / (stopLossPips * pipValuePerMicroLot)

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
	return min(base*int64(tier+1), 5_000_000), nil
}


func (m *Manager) RecordLoss(amount float64) error {
	m.resetDayIfNeeded()
	m.dailyLoss += amount
	if m.dailyLoss >= m.maxDailyLoss {
		return fmt.Errorf("daily loss limit reached: $%.2f of $%.2f", m.dailyLoss, m.maxDailyLoss)
	}
	return nil
}

func (m *Manager) RestoreLoss(amount float64) {
	m.dailyLoss = amount
}

func (m *Manager) CanTrade() bool {
	m.resetDayIfNeeded()
	return m.dailyLoss < m.maxDailyLoss
}

func (m *Manager) DailyLoss() float64 {
	m.resetDayIfNeeded()
	return m.dailyLoss
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
