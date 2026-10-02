package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

var (
	// ErrRateLimited means the hourly notification cap is reached.
	ErrRateLimited = errors.New("notification limit for the last hour reached")
	// ErrNoPushToken means no paired device has registered for push.
	ErrNoPushToken = errors.New("no device has registered for push notifications")
)

// Pusher sends one message to one device token.
type Pusher interface {
	Send(ctx context.Context, token string, msg Message) error
}

// QuietHours is a daily window, as offsets from local midnight. Start after
// End means the window crosses midnight. Equal values disable it.
type QuietHours struct {
	Start, End time.Duration
}

func (q QuietHours) enabled() bool { return q.Start != q.End }

// endAfter returns when the quiet window containing t ends, and whether t is
// inside a quiet window at all.
func (q QuietHours) endAfter(t time.Time) (time.Time, bool) {
	if !q.enabled() {
		return time.Time{}, false
	}
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	offset := t.Sub(midnight)
	switch {
	case q.Start < q.End && offset >= q.Start && offset < q.End:
		return midnight.Add(q.End), true
	case q.Start > q.End && offset >= q.Start:
		return midnight.AddDate(0, 0, 1).Add(q.End), true
	case q.Start > q.End && offset < q.End:
		return midnight.Add(q.End), true
	}
	return time.Time{}, false
}

// SenderConfig holds the sender's dependencies; every field is required.
type SenderConfig struct {
	Store      core.NotificationStore
	Pusher     Pusher
	Clock      func() time.Time
	Location   *time.Location
	MaxPerHour int
	Quiet      QuietHours
}

// Sender records every notification and delivers it now or, inside quiet
// hours, when the quiet window ends.
type Sender struct {
	cfg SenderConfig
}

func NewSender(cfg SenderConfig) (*Sender, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("notify sender: Store is required")
	case cfg.Pusher == nil:
		return nil, errors.New("notify sender: Pusher is required")
	case cfg.Clock == nil:
		return nil, errors.New("notify sender: Clock is required")
	case cfg.Location == nil:
		return nil, errors.New("notify sender: Location is required")
	case cfg.MaxPerHour < 1:
		return nil, errors.New("notify sender: MaxPerHour must be >= 1")
	}
	return &Sender{cfg: cfg}, nil
}

// Notify enforces the hourly cap, then sends n now or queues it until quiet
// hours end. The returned notification carries the resulting status.
func (s *Sender) Notify(ctx context.Context, n core.Notification) (core.Notification, error) {
	now := s.cfg.Clock()
	sent, err := s.cfg.Store.CountNotificationsSince(ctx, now.Add(-time.Hour))
	if err != nil {
		return core.Notification{}, err
	}
	if sent >= s.cfg.MaxPerHour {
		return core.Notification{}, fmt.Errorf("%w (%d per hour)", ErrRateLimited, s.cfg.MaxPerHour)
	}
	tokens, err := s.cfg.Store.ListPushTokens(ctx)
	if err != nil {
		return core.Notification{}, err
	}
	if len(tokens) == 0 {
		return core.Notification{}, ErrNoPushToken
	}
	n.SendAfter = now
	quietEnd, quiet := s.cfg.Quiet.endAfter(now.In(s.cfg.Location))
	if quiet {
		n.SendAfter = quietEnd
	}
	queued, err := s.cfg.Store.QueueNotification(ctx, n)
	if err != nil {
		return core.Notification{}, err
	}
	if quiet {
		return queued, nil
	}
	return s.deliver(ctx, queued, tokens)
}

// FlushDue delivers queued notifications whose time has come.
func (s *Sender) FlushDue(ctx context.Context) error {
	due, err := s.cfg.Store.ListDueNotifications(ctx, s.cfg.Clock())
	if err != nil {
		return err
	}
	if len(due) == 0 {
		return nil
	}
	tokens, err := s.cfg.Store.ListPushTokens(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, n := range due {
		if _, err := s.deliver(ctx, n, tokens); err != nil {
			errs = append(errs, fmt.Errorf("notification %d: %w", n.ID, err))
		}
	}
	return errors.Join(errs...)
}

// deliver sends n to every token. A token FCM no longer knows is removed. The
// notification counts as sent when at least one device received it.
func (s *Sender) deliver(ctx context.Context, n core.Notification, tokens []core.PushToken) (core.Notification, error) {
	msg := Message{Title: n.Title, Body: n.Body, ThreadID: n.ThreadID, RunID: n.RunID}
	delivered := 0
	var errs []error
	for _, token := range tokens {
		err := s.cfg.Pusher.Send(ctx, token.Token, msg)
		switch {
		case err == nil:
			delivered++
		case errors.Is(err, ErrTokenUnregistered):
			if delErr := s.cfg.Store.DeletePushToken(ctx, token.DeviceID); delErr != nil {
				errs = append(errs, delErr)
			}
			errs = append(errs, fmt.Errorf("device %s: %w", token.DeviceID, err))
		default:
			errs = append(errs, fmt.Errorf("device %s: %w", token.DeviceID, err))
		}
	}
	now := s.cfg.Clock()
	if delivered == 0 {
		if len(errs) == 0 {
			errs = append(errs, ErrNoPushToken)
		}
		cause := errors.Join(errs...)
		if err := s.cfg.Store.FinishNotification(ctx, n.ID, core.NotificationFailed, flatten(cause), now); err != nil {
			return core.Notification{}, errors.Join(cause, err)
		}
		return core.Notification{}, cause
	}
	if err := s.cfg.Store.FinishNotification(ctx, n.ID, core.NotificationSent, flatten(errors.Join(errs...)), now); err != nil {
		return core.Notification{}, err
	}
	n.Status, n.SentAt = core.NotificationSent, now
	return n, nil
}

func flatten(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), "\n", "; ")
}
