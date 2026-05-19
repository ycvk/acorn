package notifications

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

type APNSDispatcher interface {
	DispatchAPNS(ctx context.Context, req DispatchRequest) error
}

type FCMDispatcher interface {
	DispatchFCM(ctx context.Context, req DispatchRequest) error
}

type Router struct {
	APNS APNSDispatcher
	FCM  FCMDispatcher
}

func (r Router) Dispatch(ctx context.Context, req DispatchRequest) error {
	provider, err := NormalizeProvider(req.Provider)
	if err != nil {
		return err
	}
	req.Provider = provider
	switch provider {
	case ProviderAPNS:
		if r.APNS == nil {
			return fmt.Errorf("%w: apns", ErrDispatcherNotConfigured)
		}
		return r.APNS.DispatchAPNS(ctx, req)
	case ProviderFCM:
		if r.FCM == nil {
			return fmt.Errorf("%w: fcm", ErrDispatcherNotConfigured)
		}
		return r.FCM.DispatchFCM(ctx, req)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
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
