package core

import (
	"context"
	"time"
)

// EventWakeFired is recorded on a run started by a commitment wake.
const EventWakeFired = "wake.fired"

// NotificationStatus is the delivery state of an owner notification.
type NotificationStatus string

const (
	NotificationQueued NotificationStatus = "queued"
	NotificationSent   NotificationStatus = "sent"
	NotificationFailed NotificationStatus = "failed"
)

// Notification is a push message to the owner. SendAfter defers delivery
// (quiet hours).
type Notification struct {
	ID        int64
	Title     string
	Body      string
	ThreadID  string
	RunID     string
	Status    NotificationStatus
	SendAfter time.Time
	Error     string
	CreatedAt time.Time
	SentAt    time.Time
}

// PushToken is a device's FCM registration token.
type PushToken struct {
	DeviceID  string
	Token     string
	UpdatedAt time.Time
}

// NotificationStore persists push tokens and notification delivery records.
type NotificationStore interface {
	SetPushToken(ctx context.Context, deviceID, token string) error
	// ListPushTokens returns tokens of devices that are not revoked.
	ListPushTokens(ctx context.Context) ([]PushToken, error)
	DeletePushToken(ctx context.Context, deviceID string) error
	QueueNotification(ctx context.Context, n Notification) (Notification, error)
	ListDueNotifications(ctx context.Context, now time.Time) ([]Notification, error)
	FinishNotification(ctx context.Context, id int64, status NotificationStatus, errText string, at time.Time) error
	CountNotificationsSince(ctx context.Context, since time.Time) (int, error)
}
