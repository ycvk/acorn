package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

var ErrInvalidPhoneNotification = errors.New("invalid phone notification")

type PhoneNotificationInput struct {
	Key      string    `json:"key"`
	Package  string    `json:"package"`
	App      string    `json:"app"`
	Title    string    `json:"title"`
	Text     string    `json:"text"`
	PostedAt time.Time `json:"posted_at"`
}

type PhoneNotificationBatch struct {
	Notifications []PhoneNotificationInput `json:"notifications"`
}

type PhoneNotificationAccepted struct {
	Accepted int `json:"accepted"`
}

type PhoneNotificationService struct {
	store core.PhoneNotificationStore
	clock func() time.Time
}

func NewPhoneNotificationService(store core.PhoneNotificationStore, clock func() time.Time) (*PhoneNotificationService, error) {
	if store == nil || clock == nil {
		return nil, errors.New("phone notifications: store and clock are required")
	}
	return &PhoneNotificationService{store: store, clock: clock}, nil
}

func (s *PhoneNotificationService) Add(ctx context.Context, deviceID string, batch PhoneNotificationBatch) (PhoneNotificationAccepted, error) {
	if deviceID == "" {
		return PhoneNotificationAccepted{}, errors.New("phone notifications: device_id is required")
	}
	if n := len(batch.Notifications); n < 1 || n > 100 {
		return PhoneNotificationAccepted{}, fmt.Errorf("%w: notifications must contain 1–100 entries", ErrInvalidPhoneNotification)
	}
	now := s.clock()
	items := make([]core.PhoneNotification, 0, len(batch.Notifications))
	for i, item := range batch.Notifications {
		if err := validatePhoneNotification(item, now); err != nil {
			return PhoneNotificationAccepted{}, fmt.Errorf("%w: notifications[%d].%s", ErrInvalidPhoneNotification, i, err)
		}
		items = append(items, core.PhoneNotification{DeviceID: deviceID, Key: item.Key, Package: item.Package, App: item.App, Title: item.Title, Text: item.Text, PostedAt: item.PostedAt, ReceivedAt: now})
	}
	n, err := s.store.AddPhoneNotifications(ctx, items)
	return PhoneNotificationAccepted{Accepted: n}, err
}

func validatePhoneNotification(item PhoneNotificationInput, now time.Time) error {
	for _, field := range []struct {
		name, value string
		limit       int
		bytes       bool
		required    bool
	}{
		{"key", item.Key, 256, true, true}, {"package", item.Package, 256, false, true}, {"app", item.App, 256, false, true},
		{"title", item.Title, 200, false, false}, {"text", item.Text, 2000, false, false},
	} {
		if field.required && strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
		n := utf8.RuneCountInString(field.value)
		unit := "characters"
		if field.bytes {
			n = len(field.value)
			unit = "bytes"
		}
		if n > field.limit {
			return fmt.Errorf("%s must contain at most %d %s", field.name, field.limit, unit)
		}
	}
	if item.PostedAt.IsZero() {
		return errors.New("posted_at is required")
	}
	if item.PostedAt.After(now.Add(5 * time.Minute)) {
		return errors.New("posted_at must not be more than 5 minutes in the future")
	}
	return nil
}
