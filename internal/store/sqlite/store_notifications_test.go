package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	storecore "github.com/ycvk/acorn/internal/store"
)

func TestDevicePushTokenLifecycle(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	if err := store.SaveDevice(ctx, &storecore.Device{
		DeviceID:   "device_1",
		Name:       "Phone",
		Platform:   "ios",
		TokenHash:  "device-token-hash",
		CreatedAt:  now,
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("save device: %v", err)
	}

	got, err := store.UpsertDevicePushToken(ctx, &storecore.DevicePushToken{
		PushTokenID: "push_1",
		DeviceID:    "device_1",
		Provider:    "apns",
		Platform:    "ios",
		TokenValue:  "token-a",
		TokenHash:   "hash-a",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("upsert push token: %v", err)
	}
	if got.PushTokenID != "push_1" || got.TokenValue != "token-a" || got.RevokedAt != nil {
		t.Fatalf("unexpected push token: %#v", got)
	}

	updated, err := store.UpsertDevicePushToken(ctx, &storecore.DevicePushToken{
		PushTokenID: "push_ignored_on_conflict",
		DeviceID:    "device_1",
		Provider:    "apns",
		Platform:    "ios",
		TokenValue:  "token-b",
		TokenHash:   "hash-b",
		CreatedAt:   now,
		UpdatedAt:   now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("update push token: %v", err)
	}
	if updated.PushTokenID != "push_1" || updated.TokenValue != "token-b" || updated.TokenHash != "hash-b" {
		t.Fatalf("unexpected updated token: %#v", updated)
	}

	active, err := store.ListActiveDevicePushTokens(ctx)
	if err != nil {
		t.Fatalf("list active push tokens: %v", err)
	}
	if len(active) != 1 || active[0].PushTokenID != "push_1" {
		t.Fatalf("active tokens = %#v, want push_1", active)
	}

	if err := store.RevokeDevicePushToken(ctx, "device_1", "apns", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("revoke push token: %v", err)
	}
	active, err = store.ListActiveDevicePushTokens(ctx)
	if err != nil {
		t.Fatalf("list active after revoke: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active tokens after revoke = %#v, want none", active)
	}
	if err := store.RevokeDevicePushToken(ctx, "device_1", "apns", now.Add(3*time.Minute)); !errors.Is(err, storecore.ErrDevicePushTokenNotFound) {
		t.Fatalf("second revoke error = %v, want storecore.ErrDevicePushTokenNotFound", err)
	}
}

func TestNotificationAndDeliveryLifecycle(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	if err := store.SaveDevice(ctx, &storecore.Device{
		DeviceID:   "device_1",
		Name:       "Phone",
		Platform:   "ios",
		TokenHash:  "device-token-hash",
		CreatedAt:  now,
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("save device: %v", err)
	}
	if _, err := store.UpsertDevicePushToken(ctx, &storecore.DevicePushToken{
		PushTokenID: "push_1",
		DeviceID:    "device_1",
		Provider:    "apns",
		Platform:    "ios",
		TokenValue:  "token-a",
		TokenHash:   "hash-a",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("upsert push token: %v", err)
	}

	notification := &storecore.Notification{
		NotificationID: "notif_1",
		Kind:           "pending_action",
		RunID:          "run_1",
		ActionID:       "action_1",
		CreatedAt:      now,
	}
	if err := store.CreateNotification(ctx, notification); err != nil {
		t.Fatalf("create notification: %v", err)
	}
	loaded, err := store.LoadNotification(ctx, "notif_1")
	if err != nil {
		t.Fatalf("load notification: %v", err)
	}
	if loaded.Kind != "pending_action" || loaded.ActionID != "action_1" {
		t.Fatalf("unexpected notification: %#v", loaded)
	}

	delivery := &storecore.NotificationDelivery{
		DeliveryID:     "delivery_1",
		NotificationID: "notif_1",
		DeviceID:       "device_1",
		PushTokenID:    "push_1",
		Provider:       "apns",
		Status:         "pending",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.CreateNotificationDelivery(ctx, delivery); err != nil {
		t.Fatalf("create delivery: %v", err)
	}
	if err := store.UpdateNotificationDeliveryStatus(ctx, "delivery_1", "not_configured", "apns dispatcher is not configured", now.Add(time.Minute)); err != nil {
		t.Fatalf("update delivery: %v", err)
	}
	deliveries, err := store.ListNotificationDeliveries(ctx, "notif_1")
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(deliveries) != 1 || deliveries[0].Status != "not_configured" || deliveries[0].Error == "" {
		t.Fatalf("unexpected deliveries: %#v", deliveries)
	}
}
