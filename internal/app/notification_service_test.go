package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/notifications"
	storecore "github.com/ycvk/acorn/internal/store"
)

func TestNotificationServiceRegisterDevicePushTokenForCurrentDevice(t *testing.T) {
	store := &fakeNotificationStore{}
	service := NewNotificationService(store, nil)
	service.now = func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) }
	auth := &DeviceAuthContext{Device: DeviceView{DeviceID: "device_1", Platform: "ios"}}

	view, err := service.RegisterDevicePushToken(context.Background(), auth, DevicePushTokenInput{
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
	if store.pushToken.TokenValue != "apns-token" {
		t.Fatalf("stored token value = %q, want raw token for dispatch", store.pushToken.TokenValue)
	}
	if store.pushToken.TokenHash == "" || store.pushToken.TokenHash == "apns-token" {
		t.Fatalf("stored token hash should be non-empty and not raw token, got %q", store.pushToken.TokenHash)
	}
}

func TestNotificationServiceRejectsOtherDevicePushToken(t *testing.T) {
	service := NewNotificationService(&fakeNotificationStore{}, nil)
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
	store := &fakeNotificationStore{
		activeTokens: []storecore.DevicePushToken{{
			PushTokenID: "push_1",
			DeviceID:    "device_1",
			Provider:    "apns",
			TokenValue:  "token-1",
		}},
	}
	service := NewNotificationService(store, nil)
	service.now = func() time.Time { return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC) }

	if err := service.NotifyPendingAction(context.Background(), events.PendingActionRecord{
		ActionID: "action_1",
		RunID:    "run_1",
		Status:   events.PendingActionStatusPending,
	}); err != nil {
		t.Fatalf("NotifyPendingAction: %v", err)
	}
	if store.notification.Kind != notifications.KindPendingAction || store.notification.ActionID != "action_1" {
		t.Fatalf("unexpected notification: %#v", store.notification)
	}
	if len(store.deliveries) != 1 {
		t.Fatalf("delivery count = %d, want 1", len(store.deliveries))
	}
	if store.deliveries[0].Status != notifications.DeliveryStatusNotConfigured || store.deliveries[0].Error == "" {
		t.Fatalf("unexpected delivery: %#v", store.deliveries[0])
	}
}

func TestNotificationServiceDispatchesLightweightWakePayload(t *testing.T) {
	store := &fakeNotificationStore{
		activeTokens: []storecore.DevicePushToken{{
			PushTokenID: "push_1",
			DeviceID:    "device_1",
			Provider:    "apns",
			TokenValue:  "token-1",
		}},
	}
	dispatcher := &recordingPushDispatcher{}
	service := NewNotificationService(store, dispatcher)
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
	if req.Data["reload"] != "inbox" || req.Data["kind"] != notifications.KindPendingAction {
		t.Fatalf("unexpected wake data: %#v", req.Data)
	}
	if _, ok := req.Data["action_id"]; ok {
		t.Fatalf("push payload must not include action_id: %#v", req.Data)
	}
	if store.deliveries[0].Status != notifications.DeliveryStatusSent {
		t.Fatalf("delivery status = %q, want sent", store.deliveries[0].Status)
	}
}

type fakeNotificationStore struct {
	pushToken    storecore.DevicePushToken
	activeTokens []storecore.DevicePushToken
	notification storecore.Notification
	deliveries   []storecore.NotificationDelivery
}

func (s *fakeNotificationStore) UpsertDevicePushToken(_ context.Context, token *storecore.DevicePushToken) (*storecore.DevicePushToken, error) {
	s.pushToken = *token
	return &s.pushToken, nil
}

func (s *fakeNotificationStore) LoadDevicePushToken(context.Context, string, string) (*storecore.DevicePushToken, error) {
	return &s.pushToken, nil
}

func (s *fakeNotificationStore) RevokeDevicePushToken(context.Context, string, string, time.Time) error {
	return nil
}

func (s *fakeNotificationStore) ListActiveDevicePushTokens(context.Context) ([]storecore.DevicePushToken, error) {
	return append([]storecore.DevicePushToken(nil), s.activeTokens...), nil
}

func (s *fakeNotificationStore) CreateNotification(_ context.Context, notification *storecore.Notification) error {
	s.notification = *notification
	return nil
}

func (s *fakeNotificationStore) CreateNotificationDelivery(_ context.Context, delivery *storecore.NotificationDelivery) error {
	s.deliveries = append(s.deliveries, *delivery)
	return nil
}

func (s *fakeNotificationStore) UpdateNotificationDeliveryStatus(_ context.Context, deliveryID, status, errorText string, updatedAt time.Time) error {
	for i := range s.deliveries {
		if s.deliveries[i].DeliveryID == deliveryID {
			s.deliveries[i].Status = status
			s.deliveries[i].Error = errorText
			s.deliveries[i].UpdatedAt = updatedAt
			return nil
		}
	}
	return storecore.ErrNotificationNotFound
}

type recordingPushDispatcher struct {
	requests []notifications.DispatchRequest
}

func (d *recordingPushDispatcher) Dispatch(_ context.Context, req notifications.DispatchRequest) error {
	d.requests = append(d.requests, req)
	return nil
}
