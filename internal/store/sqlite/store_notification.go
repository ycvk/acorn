package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/store"
)

func (s *Store) UpsertDevicePushToken(ctx context.Context, token *store.DevicePushToken) (*store.DevicePushToken, error) {
	if token == nil {
		return nil, errors.New("device push token is nil")
	}
	if strings.TrimSpace(token.DeviceID) == "" {
		return nil, errors.New("device id is required")
	}
	if strings.TrimSpace(token.Provider) == "" {
		return nil, errors.New("push provider is required")
	}
	if strings.TrimSpace(token.TokenValue) == "" {
		return nil, errors.New("push token is required")
	}
	if strings.TrimSpace(token.PushTokenID) == "" {
		return nil, errors.New("push token id is required")
	}
	if _, err := s.LoadDevice(ctx, token.DeviceID); err != nil {
		return nil, err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO device_push_tokens(push_token_id, device_id, provider, platform, token_value, token_hash, created_at, updated_at, revoked_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, '')
		 ON CONFLICT(device_id, provider) DO UPDATE SET
		   platform = excluded.platform,
		   token_value = excluded.token_value,
		   token_hash = excluded.token_hash,
		   updated_at = excluded.updated_at,
		   revoked_at = ''`,
		token.PushTokenID,
		strings.TrimSpace(token.DeviceID),
		strings.TrimSpace(token.Provider),
		strings.TrimSpace(token.Platform),
		token.TokenValue,
		token.TokenHash,
		formatTimestamp(token.CreatedAt),
		formatTimestamp(token.UpdatedAt),
	)
	if err != nil {
		return nil, fmt.Errorf("upsert device push token: %w", err)
	}
	return s.LoadDevicePushToken(ctx, token.DeviceID, token.Provider)
}

func (s *Store) LoadDevicePushToken(ctx context.Context, deviceID, provider string) (*store.DevicePushToken, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT push_token_id, device_id, provider, platform, token_value, token_hash, created_at, updated_at, revoked_at
		 FROM device_push_tokens
		 WHERE device_id = ? AND provider = ?`,
		strings.TrimSpace(deviceID),
		strings.TrimSpace(provider),
	)
	token, err := scanDevicePushToken(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrDevicePushTokenNotFound
		}
		return nil, fmt.Errorf("load device push token: %w", err)
	}
	return token, nil
}

func (s *Store) RevokeDevicePushToken(ctx context.Context, deviceID, provider string, revokedAt time.Time) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE device_push_tokens SET revoked_at = ?, updated_at = ? WHERE device_id = ? AND provider = ? AND revoked_at = ''`,
		formatTimestamp(revokedAt),
		formatTimestamp(revokedAt),
		strings.TrimSpace(deviceID),
		strings.TrimSpace(provider),
	)
	if err != nil {
		return fmt.Errorf("revoke device push token: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revoke device push token rows affected: %w", err)
	}
	if affected == 0 {
		return store.ErrDevicePushTokenNotFound
	}
	return nil
}

func (s *Store) ListActiveDevicePushTokens(ctx context.Context) ([]store.DevicePushToken, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.push_token_id, t.device_id, t.provider, t.platform, t.token_value, t.token_hash, t.created_at, t.updated_at, t.revoked_at
		 FROM device_push_tokens t
		 JOIN devices d ON d.device_id = t.device_id
		 WHERE t.revoked_at = '' AND d.revoked_at = ''
		 ORDER BY t.created_at ASC, t.push_token_id ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list active device push tokens: %w", err)
	}
	defer rows.Close()

	items := make([]store.DevicePushToken, 0)
	for rows.Next() {
		token, err := scanDevicePushToken(rows)
		if err != nil {
			return nil, fmt.Errorf("scan device push token: %w", err)
		}
		items = append(items, *token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active device push token rows: %w", err)
	}
	return items, nil
}

func (s *Store) CreateNotification(ctx context.Context, notification *store.Notification) error {
	if notification == nil {
		return errors.New("notification is nil")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications(notification_id, kind, run_id, action_id, created_at)
		 VALUES(?, ?, ?, ?, ?)`,
		strings.TrimSpace(notification.NotificationID),
		strings.TrimSpace(notification.Kind),
		strings.TrimSpace(notification.RunID),
		strings.TrimSpace(notification.ActionID),
		formatTimestamp(notification.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
}

func (s *Store) LoadNotification(ctx context.Context, notificationID string) (*store.Notification, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT notification_id, kind, run_id, action_id, created_at
		 FROM notifications
		 WHERE notification_id = ?`,
		strings.TrimSpace(notificationID),
	)
	notification, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotificationNotFound
		}
		return nil, fmt.Errorf("load notification: %w", err)
	}
	return notification, nil
}

func (s *Store) CreateNotificationDelivery(ctx context.Context, delivery *store.NotificationDelivery) error {
	if delivery == nil {
		return errors.New("notification delivery is nil")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO notification_deliveries(delivery_id, notification_id, device_id, push_token_id, provider, status, error, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(delivery.DeliveryID),
		strings.TrimSpace(delivery.NotificationID),
		strings.TrimSpace(delivery.DeviceID),
		strings.TrimSpace(delivery.PushTokenID),
		strings.TrimSpace(delivery.Provider),
		strings.TrimSpace(delivery.Status),
		strings.TrimSpace(delivery.Error),
		formatTimestamp(delivery.CreatedAt),
		formatTimestamp(delivery.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("create notification delivery: %w", err)
	}
	return nil
}

func (s *Store) UpdateNotificationDeliveryStatus(ctx context.Context, deliveryID, status, errorText string, updatedAt time.Time) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE notification_deliveries SET status = ?, error = ?, updated_at = ? WHERE delivery_id = ?`,
		strings.TrimSpace(status),
		strings.TrimSpace(errorText),
		formatTimestamp(updatedAt),
		strings.TrimSpace(deliveryID),
	)
	if err != nil {
		return fmt.Errorf("update notification delivery status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update notification delivery rows affected: %w", err)
	}
	if affected == 0 {
		return store.ErrNotificationNotFound
	}
	return nil
}

func (s *Store) ListNotificationDeliveries(ctx context.Context, notificationID string) ([]store.NotificationDelivery, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT delivery_id, notification_id, device_id, push_token_id, provider, status, error, created_at, updated_at
		 FROM notification_deliveries
		 WHERE notification_id = ?
		 ORDER BY created_at ASC, delivery_id ASC`,
		strings.TrimSpace(notificationID),
	)
	if err != nil {
		return nil, fmt.Errorf("list notification deliveries: %w", err)
	}
	defer rows.Close()

	items := make([]store.NotificationDelivery, 0)
	for rows.Next() {
		delivery, err := scanNotificationDelivery(rows)
		if err != nil {
			return nil, fmt.Errorf("scan notification delivery: %w", err)
		}
		items = append(items, *delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list notification delivery rows: %w", err)
	}
	return items, nil
}

func scanDevicePushToken(scanner rowScanner) (*store.DevicePushToken, error) {
	var (
		token     store.DevicePushToken
		createdAt string
		updatedAt string
		revokedAt string
	)
	if err := scanner.Scan(&token.PushTokenID, &token.DeviceID, &token.Provider, &token.Platform, &token.TokenValue, &token.TokenHash, &createdAt, &updatedAt, &revokedAt); err != nil {
		return nil, err
	}
	created, err := parseTimestamp(fixedTimestampLayout, createdAt, "device_push_token.created_at")
	if err != nil {
		return nil, err
	}
	updated, err := parseTimestamp(fixedTimestampLayout, updatedAt, "device_push_token.updated_at")
	if err != nil {
		return nil, err
	}
	revoked, err := parseOptionalTimestamp(revokedAt, "device_push_token.revoked_at")
	if err != nil {
		return nil, err
	}
	token.CreatedAt = created
	token.UpdatedAt = updated
	token.RevokedAt = revoked
	return &token, nil
}

func scanNotification(scanner rowScanner) (*store.Notification, error) {
	var (
		notification store.Notification
		createdAt    string
	)
	if err := scanner.Scan(&notification.NotificationID, &notification.Kind, &notification.RunID, &notification.ActionID, &createdAt); err != nil {
		return nil, err
	}
	created, err := parseTimestamp(fixedTimestampLayout, createdAt, "notification.created_at")
	if err != nil {
		return nil, err
	}
	notification.CreatedAt = created
	return &notification, nil
}

func scanNotificationDelivery(scanner rowScanner) (*store.NotificationDelivery, error) {
	var (
		delivery  store.NotificationDelivery
		createdAt string
		updatedAt string
	)
	if err := scanner.Scan(&delivery.DeliveryID, &delivery.NotificationID, &delivery.DeviceID, &delivery.PushTokenID, &delivery.Provider, &delivery.Status, &delivery.Error, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	created, err := parseTimestamp(fixedTimestampLayout, createdAt, "notification_delivery.created_at")
	if err != nil {
		return nil, err
	}
	updated, err := parseTimestamp(fixedTimestampLayout, updatedAt, "notification_delivery.updated_at")
	if err != nil {
		return nil, err
	}
	delivery.CreatedAt = created
	delivery.UpdatedAt = updated
	return &delivery, nil
}
