package event

import "time"

type Event struct {
	ID        string
	EventType string
	Detail    map[string]any
	ElapsedMs int64
	CreatedAt time.Time
}
