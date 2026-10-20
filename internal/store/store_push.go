package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// registerPushByInstallation (17 to 18) replaces push_tokens with
// push_installations. FCM addresses an app by its Firebase installation ID,
// which a registration token is not, so the tokens are dropped; each app
// registers its installation ID again when it next starts.
func registerPushByInstallation(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{`DROP TABLE push_tokens`, pushInstallationsTable} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// SetPushInstallation binds fid to deviceID. An FID names one app install,
// so it moves off any device that held it before: re-pairing the same phone
// must not deliver every notification twice.
func (s *Store) SetPushInstallation(ctx context.Context, deviceID, fid string) (err error) {
	if strings.TrimSpace(deviceID) == "" || strings.TrimSpace(fid) == "" {
		return errors.New("set push installation: device id and fid are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("set push installation for %s: begin: %w", deviceID, err)
	}
	defer rollbackOnErr(tx, &err, "set push installation")
	if _, err := tx.ExecContext(ctx, `DELETE FROM push_installations WHERE fid = ? AND device_id <> ?`, fid, deviceID); err != nil {
		return fmt.Errorf("set push installation for %s: release from other devices: %w", deviceID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO push_installations(device_id, fid, updated_at) VALUES(?, ?, ?)
		 ON CONFLICT(device_id) DO UPDATE SET fid = excluded.fid, updated_at = excluded.updated_at`,
		deviceID, fid, formatTimestamp(time.Now().UTC())); err != nil {
		return fmt.Errorf("set push installation for %s: %w", deviceID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set push installation for %s: commit: %w", deviceID, err)
	}
	return nil
}

func (s *Store) ListPushInstallations(ctx context.Context) ([]core.PushInstallation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.device_id, p.fid, p.updated_at FROM push_installations p
		 JOIN devices d ON d.device_id = p.device_id
		 WHERE d.revoked_at = '' ORDER BY p.device_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list push installations: %w", err)
	}
	var installations []core.PushInstallation
	err = scanRows(rows, func(scan func(...any) error) error {
		var (
			p       core.PushInstallation
			updated string
		)
		if err := scan(&p.DeviceID, &p.FID, &updated); err != nil {
			return err
		}
		var err error
		p.UpdatedAt, err = parseTimestamp(fixedTimestampLayout, updated, "push_installations.updated_at")
		installations = append(installations, p)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list push installations: %w", err)
	}
	return installations, nil
}

func (s *Store) DeletePushInstallation(ctx context.Context, deviceID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM push_installations WHERE device_id = ?`, deviceID); err != nil {
		return fmt.Errorf("delete push installation for %s: %w", deviceID, err)
	}
	return nil
}

const notificationColumns = `id, title, body, thread_id, run_id, action_id, browser_url, status, send_after, error_text, created_at, sent_at`

func (s *Store) QueueNotification(ctx context.Context, n core.Notification) (core.Notification, error) {
	if strings.TrimSpace(n.Title) == "" || strings.TrimSpace(n.Body) == "" {
		return core.Notification{}, errors.New("queue notification: title and body are required")
	}
	if n.SendAfter.IsZero() {
		return core.Notification{}, errors.New("queue notification: send_after is required")
	}
	if n.CreatedAt.IsZero() {
		return core.Notification{}, errors.New("queue notification: created_at is required")
	}
	n.Status = core.NotificationQueued
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications(title, body, thread_id, run_id, action_id, browser_url, status, send_after, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.Title, n.Body, n.ThreadID, n.RunID, n.ActionID, n.BrowserURL, string(n.Status), formatTimestamp(n.SendAfter), formatTimestamp(n.CreatedAt))
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
		if err := scan(&n.ID, &n.Title, &n.Body, &n.ThreadID, &n.RunID, &n.ActionID, &n.BrowserURL, &status, &sendAfter, &n.Error, &created, &sentAt); err != nil {
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
