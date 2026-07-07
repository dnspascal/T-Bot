package notify

import "context"

type Channel interface {
	Name() string
	Send(ctx context.Context, recipientID string, eventType EventType, payload any) error
}
