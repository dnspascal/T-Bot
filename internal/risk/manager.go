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
}

func New(riskPercent, maxDailyLoss float64) *Manager {
	return &Manager{
		riskPercent:  riskPercent,
		maxDailyLoss: maxDailyLoss,
		dayStart:     today(),
	}
}

var dsmLocation, _ = time.LoadLocation("Africa/Dar_es_Salaam")


func (m *Manager) PositionSize(balance, stopLossPips float64) (int64, error) {
	if stopLossPips < 5 {
		return 0, fmt.Errorf("stop loss too tight: %.1f pips (minimum 5)", stopLossPips)
	}

	riskAmount := balance * (m.riskPercent / 100)

	pipValuePerMicroLot := 0.10
	microLots := riskAmount / (stopLossPips * pipValuePerMicroLot)

	volume := int64(microLots * 100000)

	volume = max(100000, min(volume, 5000000))

	return volume, nil
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
