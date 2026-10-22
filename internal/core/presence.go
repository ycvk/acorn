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
	// NotificationDropped is a queued push for a pending action the owner
	// decided before quiet hours ended; it is never delivered.
	NotificationDropped NotificationStatus = "dropped"
)

// Notification is a push message to the owner. SendAfter defers delivery
// (quiet hours). ActionID marks a push announcing a pending action, which the
// app shows with the action's own controls. BrowserURL makes a tap open the
// agent's browser at that page over the conversation.
type Notification struct {
	ID         int64
	Title      string
	Body       string
	ThreadID   string
	RunID      string
	ActionID   string
	BrowserURL string
	Status     NotificationStatus
	SendAfter  time.Time
	Error      string
	CreatedAt  time.Time
	SentAt     time.Time
}

// PushInstallation is the Firebase installation ID (FID) under which a
// device's app is registered with FCM.
type PushInstallation struct {
	DeviceID  string
	FID       string
	UpdatedAt time.Time
}

// NotificationStore persists push installations and notification delivery records.
type NotificationStore interface {
	SetPushInstallation(ctx context.Context, deviceID, fid string) error
	// ListPushInstallations returns installations of devices that are not revoked.
	ListPushInstallations(ctx context.Context) ([]PushInstallation, error)
	DeletePushInstallation(ctx context.Context, deviceID string) error
	QueueNotification(ctx context.Context, n Notification) (Notification, error)
	ListDueNotifications(ctx context.Context, now time.Time) ([]Notification, error)
	FinishNotification(ctx context.Context, id int64, status NotificationStatus, errText string, at time.Time) error
	CountNotificationsSince(ctx context.Context, since time.Time) (int, error)
}
