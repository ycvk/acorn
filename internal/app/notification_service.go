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
	"github.com/ycvk/acorn/internal/store"
)

func pendingActionOptionsFromEventOptions(items []events.PendingActionOption) []PendingActionOption {
	if len(items) == 0 {
		return nil
	}
	out := make([]PendingActionOption, 0, len(items))
	for _, item := range items {
		out = append(out, PendingActionOption{
			ID:          strings.TrimSpace(item.ID),
			Label:       strings.TrimSpace(item.Label),
			Description: strings.TrimSpace(item.Description),
		})
	}
	return out
}

func (s *InboxService) projectRunSummary(ctx context.Context, record events.RunRecord) (RunSummary, error) {
	status, err := projectRunStatus(record.Status)
	if err != nil {
		return RunSummary{}, err
	}
	mode, err := projectRunMode(record.OrchestrationMode)
	if err != nil {
		return RunSummary{}, err
	}
	session, err := s.store.LoadSession(ctx, record.SessionID)
	if err != nil {
		return RunSummary{}, err
	}
	return RunSummary{
		RunID:          record.RunID,
		ThreadID:       record.SessionID,
		ThreadTitle:    runSummaryThreadTitle(*session, record),
		Status:         status,
		Mode:           mode,
		Preview:        runSummaryPreview(record),
		LastEventLabel: runSummaryLastEventLabel(record.Status),
		AttentionLevel: runSummaryAttentionLevel(record.Status),
		DurationMS:     runSummaryDurationMS(record),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}, nil
}

func runSummaryThreadTitle(session events.SessionRecord, run events.RunRecord) string {
	title := strings.TrimSpace(session.Title)
	if title != "" {
		return truncateRunes(title, runSummaryTitleMaxRunes)
	}
	title = strings.TrimSpace(run.Input)
	if title != "" {
		return truncateRunes(compactWhitespace(title), runSummaryTitleMaxRunes)
	}
	return "Untitled thread"
}

func runSummaryPreview(record events.RunRecord) string {
	switch record.Status {
	case events.RunStatusFailed:
		if preview := previewText(record.Error); preview != "" {
			return preview
		}
		if preview := previewText(record.Output); preview != "" {
			return preview
		}
	case events.RunStatusSucceeded:
		if preview := previewText(record.Output); preview != "" {
			return preview
		}
	case events.RunStatusInterrupted, events.RunStatusRunning:
		if preview := previewText(record.Input); preview != "" {
			return preview
		}
	}
	if preview := previewText(record.Input); preview != "" {
		return preview
	}
	if preview := previewText(record.Output); preview != "" {
		return preview
	}
	return ""
}

func previewText(value string) string {
	return truncateRunes(compactWhitespace(value), runSummaryPreviewMaxRunes)
}

func compactWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func truncateRunes(value string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

func runSummaryLastEventLabel(status events.RunStatus) string {
	switch status {
	case events.RunStatusRunning:
		return "Run is running"
	case events.RunStatusSucceeded:
		return "Run completed"
	case events.RunStatusInterrupted:
		return "Run interrupted"
	case events.RunStatusFailed:
		return "Run failed"
	default:
		return string(status)
	}
}

func runSummaryAttentionLevel(status events.RunStatus) string {
	switch status {
	case events.RunStatusRunning:
		return "running"
	case events.RunStatusFailed:
		return "failed"
	case events.RunStatusInterrupted:
		return "needs_action"
	default:
		return "normal"
	}
}

func runSummaryDurationMS(record events.RunRecord) int64 {
	if record.UpdatedAt.IsZero() || record.CreatedAt.IsZero() {
		return 0
	}
	duration := record.UpdatedAt.Sub(record.CreatedAt)
	if duration < 0 {
		return 0
	}
	return duration.Milliseconds()
}

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
	record, err := s.store.UpsertDevicePushToken(ctx, &store.DevicePushToken{
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
	notification := &store.Notification{
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

func (s *NotificationService) createAndDispatchDelivery(ctx context.Context, notification store.Notification, token store.DevicePushToken) error {
	now := s.now()
	deliveryID, err := generatePrefixedID(s.rand, "delivery", 16)
	if err != nil {
		return err
	}
	delivery := &store.NotificationDelivery{
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

func devicePushTokenView(record store.DevicePushToken) DevicePushTokenView {
	return DevicePushTokenView{
		DeviceID:  record.DeviceID,
		Provider:  record.Provider,
		Platform:  record.Platform,
		UpdatedAt: record.UpdatedAt,
	}
}
