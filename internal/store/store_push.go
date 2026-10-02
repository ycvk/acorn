package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) SetPushToken(ctx context.Context, deviceID, token string) error {
	if strings.TrimSpace(deviceID) == "" || strings.TrimSpace(token) == "" {
		return errors.New("set push token: device id and token are required")
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO push_tokens(device_id, token, updated_at) VALUES(?, ?, ?)
		 ON CONFLICT(device_id) DO UPDATE SET token = excluded.token, updated_at = excluded.updated_at`,
		deviceID, token, formatTimestamp(time.Now().UTC())); err != nil {
		return fmt.Errorf("set push token for %s: %w", deviceID, err)
	}
	return nil
}

func (s *Store) ListPushTokens(ctx context.Context) ([]core.PushToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.device_id, p.token, p.updated_at FROM push_tokens p
		 JOIN devices d ON d.device_id = p.device_id
		 WHERE d.revoked_at = '' ORDER BY p.device_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list push tokens: %w", err)
	}
	var tokens []core.PushToken
	err = scanRows(rows, func(scan func(...any) error) error {
		var (
			t       core.PushToken
			updated string
		)
		if err := scan(&t.DeviceID, &t.Token, &updated); err != nil {
			return err
		}
		var err error
		t.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, updated, "push_tokens.updated_at")
		tokens = append(tokens, t)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list push tokens: %w", err)
	}
	return tokens, nil
}

func (s *Store) DeletePushToken(ctx context.Context, deviceID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM push_tokens WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("delete push token for %s: %w", deviceID, err)
	}
	return nil
}

const notificationColumns = `id, title, body, thread_id, run_id, status, send_after, error_text, created_at, sent_at`

func (s *Store) QueueNotification(ctx context.Context, n core.Notification) (core.Notification, error) {
	if strings.TrimSpace(n.Title) == "" || strings.TrimSpace(n.Body) == "" {
		return core.Notification{}, errors.New("queue notification: title and body are required")
	}
	if n.SendAfter.IsZero() {
		return core.Notification{}, errors.New("queue notification: send_after is required")
	}
	n.Status = core.NotificationQueued
	n.CreatedAt = time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications(title, body, thread_id, run_id, status, send_after, created_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		n.Title, n.Body, n.ThreadID, n.RunID, string(n.Status), formatTimestamp(n.SendAfter), formatTimestamp(n.CreatedAt))
	if err != nil {
		return core.Notification{}, fmt.Errorf("queue notification: %w", err)
	}
	if n.ID, err = result.LastInsertId(); err != nil {
		return core.Notification{}, fmt.Errorf("queue notification id: %w", err)
	}
	return n, nil
}

func (s *Store) ListDueNotifications(ctx context.Context, now time.Time) ([]core.Notification, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+notificationColumns+` FROM notifications WHERE status = ? AND send_after <= ? ORDER BY send_after ASC, id ASC`,
		string(core.NotificationQueued), formatTimestamp(now))
	if err != nil {
		return nil, fmt.Errorf("list due notifications: %w", err)
	}
	var out []core.Notification
	err = scanRows(rows, func(scan func(...any) error) error {
		var (
			n                          core.Notification
			status, sendAfter, created string
			sentAt                     string
		)
		if err := scan(&n.ID, &n.Title, &n.Body, &n.ThreadID, &n.RunID, &status, &sendAfter, &n.Error, &created, &sentAt); err != nil {
			return err
		}
		n.Status = core.NotificationStatus(status)
		var err error
		if n.SendAfter, err = parseTimestamp(fixedTimestampLayout, sendAfter, "notifications.send_after"); err != nil {
			return err
		}
		if n.CreatedAt, err = parseTimestamp(fixedTimestampLayout, created, "notifications.created_at"); err != nil {
			return err
		}
		if n.SentAt, err = parseOptionalTime(sentAt, "notifications.sent_at"); err != nil {
			return err
		}
		out = append(out, n)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list due notifications: %w", err)
	}
	return out, nil
}

func (s *Store) FinishNotification(ctx context.Context, id int64, status core.NotificationStatus, errText string, at time.Time) error {
	if status != core.NotificationSent && status != core.NotificationFailed {
		return fmt.Errorf("finish notification %d: unsupported status %q", id, status)
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET status = ?, error_text = ?, sent_at = ? WHERE id = ?`,
		string(status), errText, formatTimestamp(at), id)
	if err != nil {
		return fmt.Errorf("finish notification %d: %w", id, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("finish notification %d rows affected: %w", id, err)
	}
	if affected == 0 {
		return fmt.Errorf("finish notification %d: not found", id)
	}
	return nil
}

// CountNotificationsSince counts notifications created at or after since,
// whatever their delivery state, so queued ones count toward the hourly cap.
func (s *Store) CountNotificationsSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE created_at >= ?`, formatTimestamp(since)).Scan(&n); err != nil {
		return 0, fmt.Errorf("count notifications: %w", err)
	}
	return n, nil
}
