package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/store"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
)

func openNotificationTestStore(t *testing.T) *storesqlite.Store {
	t.Helper()
	s, err := storesqlite.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func setupNotificationTestDeviceAndToken(t *testing.T, s *storesqlite.Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.SaveDevice(ctx, &store.Device{
		DeviceID:   "device_1",
		Name:       "Test Device",
		Platform:   "ios",
		TokenHash:  "hash",
		CreatedAt:  now,
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}
	if _, err := s.UpsertDevicePushToken(ctx, &store.DevicePushToken{
		PushTokenID: "push_1",
		DeviceID:    "device_1",
		Provider:    "apns",
		Platform:    "ios",
		TokenValue:  "token-1",
		TokenHash:   "hash-1",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("UpsertDevicePushToken: %v", err)
	}
}

func TestNotificationServiceRegisterDevicePushTokenForCurrentDevice(t *testing.T) {
	s := openNotificationTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.SaveDevice(ctx, &store.Device{
		DeviceID:   "device_1",
		Name:       "Test Device",
		Platform:   "ios",
		TokenHash:  "hash",
		CreatedAt:  now,
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("SaveDevice: %v", err)
	}

	service := NewNotificationService(s, nil)
	service.now = func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) }
	auth := &DeviceAuthContext{Device: DeviceView{DeviceID: "device_1", Platform: "ios"}}

	view, err := service.RegisterDevicePushToken(ctx, auth, DevicePushTokenInput{
		DeviceID: "device_1",
		Provider: "apns",
		Token:    "apns-token",
	})
	if err != nil {
		t.Fatalf("RegisterDevicePushToken: %v", err)
	}
	if view.DeviceID != "device_1" || view.Provider != "apns" || view.Platform != "ios" {
		t.Fatalf("unexpected view: %#v", view)
	}

	stored, err := s.LoadDevicePushToken(context.Background(), "device_1", "apns")
	if err != nil {
		t.Fatalf("LoadDevicePushToken: %v", err)
	}
	if stored.TokenValue != "apns-token" {
		t.Fatalf("stored token value = %q, want raw token for dispatch", stored.TokenValue)
	}
	if stored.TokenHash == "" || stored.TokenHash == "apns-token" {
		t.Fatalf("stored token hash should be non-empty and not raw token, got %q", stored.TokenHash)
	}
}

func TestNotificationServiceRejectsOtherDevicePushToken(t *testing.T) {
	service := NewNotificationService(openNotificationTestStore(t), nil)
	auth := &DeviceAuthContext{Device: DeviceView{DeviceID: "device_1", Platform: "ios"}}

	_, err := service.RegisterDevicePushToken(context.Background(), auth, DevicePushTokenInput{
		DeviceID: "device_2",
		Provider: "apns",
		Token:    "apns-token",
	})
	if !errors.Is(err, ErrDevicePushTokenForbidden) {
		t.Fatalf("RegisterDevicePushToken error = %v, want ErrDevicePushTokenForbidden", err)
	}
}

func TestNotificationServiceNotifyPendingActionRecordsNotConfiguredDelivery(t *testing.T) {
	s := openNotificationTestStore(t)
	setupNotificationTestDeviceAndToken(t, s)

	service := NewNotificationService(s, nil)
	service.now = func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) }

	if err := service.NotifyPendingAction(context.Background(), events.PendingActionRecord{
		ActionID: "action_1",
		RunID:    "run_1",
		Status:   events.PendingActionStatusPending,
	}); err != nil {
		t.Fatalf("NotifyPendingAction: %v", err)
	}
}

func TestNotificationServiceDispatchesLightweightWakePayload(t *testing.T) {
	s := openNotificationTestStore(t)
	setupNotificationTestDeviceAndToken(t, s)

	dispatcher := &recordingPushDispatcher{}
	service := NewNotificationService(s, dispatcher)
	service.now = func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) }

	if err := service.NotifyPendingAction(context.Background(), events.PendingActionRecord{
		ActionID: "action_1",
		RunID:    "run_1",
		Status:   events.PendingActionStatusPending,
	}); err != nil {
		t.Fatalf("NotifyPendingAction: %v", err)
	}
	if len(dispatcher.requests) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.requests))
	}
	req := dispatcher.requests[0]
	if req.Data["reload"] != "inbox" || req.Data["kind"] != KindPendingAction {
		t.Fatalf("unexpected wake data: %#v", req.Data)
	}
	if _, ok := req.Data["action_id"]; ok {
		t.Fatalf("push payload must not include action_id: %#v", req.Data)
	}
}

type recordingPushDispatcher struct {
	requests []DispatchRequest
}

func (d *recordingPushDispatcher) Dispatch(_ context.Context, req DispatchRequest) error {
	d.requests = append(d.requests, req)
	return nil
}
