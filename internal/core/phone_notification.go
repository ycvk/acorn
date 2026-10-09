package core

import (
	"context"
	"time"
)

type PhoneNotification struct {
	ID         int64
	DeviceID   string
	Key        string
	Package    string
	App        string
	Title      string
	Text       string
	PostedAt   time.Time
	ReceivedAt time.Time
}

type PhoneNotificationPage struct {
	Items []PhoneNotification
	Total int
}

type PhoneNotificationCount struct {
	Package string `json:"package"`
	App     string `json:"app"`
	Count   int    `json:"count"`
}

type PhoneNotificationStore interface {
	AddPhoneNotifications(ctx context.Context, items []PhoneNotification) (int, error)
	// ListPhoneNotifications uses [since, until), newest first, with an exact total.
	ListPhoneNotifications(ctx context.Context, since, until time.Time, limit int) (PhoneNotificationPage, error)
	PhoneNotificationCounts(ctx context.Context, since time.Time) ([]PhoneNotificationCount, error)
	PrunePhoneNotifications(ctx context.Context, before time.Time) (int, error)
}
