package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ProviderAPNS = "apns"
	ProviderFCM  = "fcm"

	KindPendingAction = "pending_action"

	DeliveryStatusPending       = "pending"
	DeliveryStatusSent          = "sent"
	DeliveryStatusFailed        = "failed"
	DeliveryStatusNotConfigured = "not_configured"
)

var (
	ErrUnsupportedProvider     = errors.New("unsupported push provider")
	ErrDispatcherNotConfigured = errors.New("push dispatcher not configured")
)

type DispatchRequest struct {
	Provider       string
	Token          string
	NotificationID string
	Kind           string
	CreatedAt      time.Time
	Data           map[string]string
}

type PushDispatcher interface {
	Dispatch(ctx context.Context, req DispatchRequest) error
}

type notificationRouter struct {
	dispatchers map[string]PushDispatcher
}

func newNotificationRouter(dispatchers map[string]PushDispatcher) notificationRouter {
	return notificationRouter{dispatchers: dispatchers}
}

func (r notificationRouter) Dispatch(ctx context.Context, req DispatchRequest) error {
	provider, err := NormalizeProvider(req.Provider)
	if err != nil {
		return err
	}
	req.Provider = provider
	d, ok := r.dispatchers[provider]
	if !ok || d == nil {
		return fmt.Errorf("%w: %s", ErrDispatcherNotConfigured, provider)
	}
	return d.Dispatch(ctx, req)
}

func NormalizeProvider(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case ProviderAPNS:
		return ProviderAPNS, nil
	case ProviderFCM:
		return ProviderFCM, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
}

func WakeData(notificationID, kind string) map[string]string {
	return map[string]string{
		"notification_id": strings.TrimSpace(notificationID),
		"kind":            strings.TrimSpace(kind),
		"reload":          "inbox",
	}
}
