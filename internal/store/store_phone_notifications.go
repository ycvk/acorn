package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

var _ core.PhoneNotificationStore = (*Store)(nil)

func (s *Store) AddPhoneNotifications(ctx context.Context, items []core.PhoneNotification) (count int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("add phone notifications: %w", err)
	}
	defer rollbackOnErr(tx, &err, "add phone notifications")
	for _, item := range items {
		if item.DeviceID == "" || item.Key == "" || item.Package == "" || item.App == "" || item.ReceivedAt.IsZero() || item.PostedAt.IsZero() {
			return 0, errors.New("phone notification: device_id, key, package, app, posted_at and received_at are required")
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO phone_notifications(device_id,notification_key,package,app,title,text,posted_at,received_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(device_id,notification_key,posted_at) DO NOTHING`, item.DeviceID, item.Key, item.Package, item.App, item.Title, item.Text, formatTimestamp(item.PostedAt), formatTimestamp(item.ReceivedAt))
		if err != nil {
			return 0, fmt.Errorf("add phone notification: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("phone notification rows affected: %w", err)
		}
		count += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit phone notifications: %w", err)
	}
	return count, nil
}

func (s *Store) ListPhoneNotifications(ctx context.Context, since, until time.Time, limit int) (core.PhoneNotificationPage, error) {
	page := core.PhoneNotificationPage{Items: []core.PhoneNotification{}}
	if limit < 1 {
		return page, errors.New("list phone notifications: limit must be positive")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,device_id,notification_key,package,app,title,text,posted_at,received_at,COUNT(*) OVER() FROM phone_notifications WHERE received_at >= ? AND received_at < ? ORDER BY received_at DESC,id DESC LIMIT ?`, formatTimestamp(since), formatTimestamp(until), limit)
	if err != nil {
		return page, fmt.Errorf("list phone notifications: %w", err)
	}
	err = scanRows(rows, func(scan func(...any) error) error {
		var item core.PhoneNotification
		var posted, received string
		if err := scan(&item.ID, &item.DeviceID, &item.Key, &item.Package, &item.App, &item.Title, &item.Text, &posted, &received, &page.Total); err != nil {
			return err
		}
		var err error
		if item.PostedAt, err = parseTimestamp(fixedTimestampLayout, posted, "phone_notifications.posted_at"); err != nil {
			return err
		}
		if item.ReceivedAt, err = parseTimestamp(fixedTimestampLayout, received, "phone_notifications.received_at"); err != nil {
			return err
		}
		page.Items = append(page.Items, item)
		return nil
	})
	return page, err
}

func (s *Store) PhoneNotificationCounts(ctx context.Context, since time.Time) ([]core.PhoneNotificationCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT package,app,COUNT(*) FROM phone_notifications WHERE received_at >= ? GROUP BY package,app ORDER BY package,app`, formatTimestamp(since))
	if err != nil {
		return nil, fmt.Errorf("count phone notifications: %w", err)
	}
	out := []core.PhoneNotificationCount{}
	err = scanRows(rows, func(scan func(...any) error) error {
		var item core.PhoneNotificationCount
		if err := scan(&item.Package, &item.App, &item.Count); err != nil {
			return err
		}
		out = append(out, item)
		return nil
	})
	return out, err
}

func (s *Store) PrunePhoneNotifications(ctx context.Context, before time.Time) (int, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM phone_notifications WHERE received_at < ?`, formatTimestamp(before))
	if err != nil {
		return 0, fmt.Errorf("prune phone notifications: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune phone notifications rows affected: %w", err)
	}
	return int(n), nil
}
