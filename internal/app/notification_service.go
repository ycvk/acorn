package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/notifications"
	storecore "github.com/ycvk/acorn/internal/store"
)

var (
	ErrDevicePushTokenForbidden = errors.New("device push token belongs to another device")
	ErrInvalidPushProvider      = errors.New("invalid push provider")
)

type PushDispatcher interface {
	Dispatch(ctx context.Context, req notifications.DispatchRequest) error
}

type DevicePushTokenInput struct {
	DeviceID string
	Provider string
	Platform string
	Token    string
}

type DevicePushTokenView struct {
	DeviceID  string
	Provider  string
	Platform  string
	UpdatedAt time.Time
}

type NotificationService struct {
	store      notificationStore
	dispatcher PushDispatcher
	now        func() time.Time
	rand       io.Reader
}

func NewNotificationService(store notificationStore, dispatcher PushDispatcher) *NotificationService {
	return &NotificationService{
		store:      store,
		dispatcher: dispatcher,
		now:        func() time.Time { return time.Now().UTC() },
		rand:       rand.Reader,
	}
}

func (s *NotificationService) RegisterDevicePushToken(ctx context.Context, auth *DeviceAuthContext, input DevicePushTokenInput) (*DevicePushTokenView, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("notification store is nil")
	}
	if auth == nil {
		return nil, ErrUnauthenticated
	}
	deviceID := strings.TrimSpace(input.DeviceID)
	if deviceID == "" {
		return nil, ErrDeviceNotFound
	}
	if auth.Device.DeviceID != deviceID {
		return nil, ErrDevicePushTokenForbidden
	}
	provider, err := notifications.NormalizeProvider(input.Provider)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidPushProvider, input.Provider)
	}
	tokenValue := strings.TrimSpace(input.Token)
	if tokenValue == "" {
		return nil, errors.New("push token is required")
	}
	platform := strings.TrimSpace(input.Platform)
	if platform == "" {
		platform = auth.Device.Platform
	}
	now := s.now()
	pushTokenID, err := generatePrefixedID(s.rand, "push", 16)
	if err != nil {
		return nil, err
	}
	record, err := s.store.UpsertDevicePushToken(ctx, &storecore.DevicePushToken{
		PushTokenID: pushTokenID,
		DeviceID:    deviceID,
		Provider:    provider,
		Platform:    platform,
		TokenValue:  tokenValue,
		TokenHash:   hashSecret(tokenValue),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return nil, err
	}
	view := devicePushTokenView(*record)
	return &view, nil
}

func (s *NotificationService) RevokeDevicePushToken(ctx context.Context, auth *DeviceAuthContext, deviceID, provider string) error {
	if s == nil || s.store == nil {
		return errors.New("notification store is nil")
	}
	if auth == nil {
		return ErrUnauthenticated
	}
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return ErrDeviceNotFound
	}
	if auth.Device.DeviceID != deviceID {
		return ErrDevicePushTokenForbidden
	}
	normalizedProvider, err := notifications.NormalizeProvider(provider)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidPushProvider, provider)
	}
	return s.store.RevokeDevicePushToken(ctx, deviceID, normalizedProvider, s.now())
}

func (s *NotificationService) NotifyPendingAction(ctx context.Context, action events.PendingActionRecord) error {
	if s == nil || s.store == nil {
		return errors.New("notification store is nil")
	}
	if strings.TrimSpace(action.ActionID) == "" {
		return errors.New("pending action id is required")
	}
	if strings.TrimSpace(action.RunID) == "" {
		return errors.New("pending action run id is required")
	}
	now := s.now()
	notificationID, err := generatePrefixedID(s.rand, "notif", 16)
	if err != nil {
		return err
	}
	notification := &storecore.Notification{
		NotificationID: notificationID,
		Kind:           notifications.KindPendingAction,
		RunID:          action.RunID,
		ActionID:       action.ActionID,
		CreatedAt:      now,
	}
	if err := s.store.CreateNotification(ctx, notification); err != nil {
		return err
	}
	tokens, err := s.store.ListActiveDevicePushTokens(ctx)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		if err := s.createAndDispatchDelivery(ctx, *notification, token); err != nil {
			return err
		}
	}
	return nil
}

func (s *NotificationService) createAndDispatchDelivery(ctx context.Context, notification storecore.Notification, token storecore.DevicePushToken) error {
	now := s.now()
	deliveryID, err := generatePrefixedID(s.rand, "delivery", 16)
	if err != nil {
		return err
	}
	delivery := &storecore.NotificationDelivery{
		DeliveryID:     deliveryID,
		NotificationID: notification.NotificationID,
		DeviceID:       token.DeviceID,
		PushTokenID:    token.PushTokenID,
		Provider:       token.Provider,
		Status:         notifications.DeliveryStatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.store.CreateNotificationDelivery(ctx, delivery); err != nil {
		return err
	}
	status := notifications.DeliveryStatusSent
	errorText := ""
	if s.dispatcher == nil {
		status = notifications.DeliveryStatusNotConfigured
		errorText = notifications.ErrDispatcherNotConfigured.Error()
	} else if err := s.dispatcher.Dispatch(ctx, notifications.DispatchRequest{
		Provider:       token.Provider,
		Token:          token.TokenValue,
		NotificationID: notification.NotificationID,
		Kind:           notification.Kind,
		CreatedAt:      notification.CreatedAt,
		Data:           notifications.WakeData(notification.NotificationID, notification.Kind),
	}); err != nil {
		if errors.Is(err, notifications.ErrDispatcherNotConfigured) {
			status = notifications.DeliveryStatusNotConfigured
		} else {
			status = notifications.DeliveryStatusFailed
		}
		errorText = err.Error()
	}
	if err := s.store.UpdateNotificationDeliveryStatus(ctx, deliveryID, status, errorText, s.now()); err != nil {
		return err
	}
	if status == notifications.DeliveryStatusFailed {
		return errors.New(errorText)
	}
	return nil
}

func devicePushTokenView(record storecore.DevicePushToken) DevicePushTokenView {
	return DevicePushTokenView{
		DeviceID:  record.DeviceID,
		Provider:  record.Provider,
		Platform:  record.Platform,
		UpdatedAt: record.UpdatedAt,
	}
}
