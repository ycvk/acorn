package app

import (
	"context"
	"fmt"

	"github.com/ycvk/acorn/internal/events"
	storecore "github.com/ycvk/acorn/internal/store"
)

type notifyingPendingActionStore struct {
	PendingActionCreateStore
	notifications *NotificationService
}

type PendingActionCreateStore interface {
	ListPendingActions(ctx context.Context, limit int) ([]events.PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*events.PendingActionRecord, error)
	LoadRun(ctx context.Context, runID string) (*events.RunRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status events.PendingActionStatus, mode events.PendingActionDecisionMode, decisionJSON string) (*events.PendingActionRecord, error)
	SyncDecisionMessageForPendingAction(ctx context.Context, actionID string) error
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
	CreatePendingAction(context.Context, storecore.CreatePendingActionInput) (*events.PendingActionRecord, error)
}

func NewNotifyingPendingActionStore(store PendingActionCreateStore, notifications *NotificationService) PendingActionCreateStore {
	if notifications == nil {
		return store
	}
	return &notifyingPendingActionStore{
		PendingActionCreateStore: store,
		notifications:            notifications,
	}
}

func (s *notifyingPendingActionStore) CreatePendingAction(ctx context.Context, input storecore.CreatePendingActionInput) (*events.PendingActionRecord, error) {
	record, err := s.PendingActionCreateStore.CreatePendingAction(ctx, input)
	if err != nil {
		return nil, err
	}
	if record.Status == events.PendingActionStatusPending {
		if err := s.notifications.NotifyPendingAction(ctx, *record); err != nil {
			return nil, fmt.Errorf("notify pending action: %w", err)
		}
	}
	return record, nil
}
