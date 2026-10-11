package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

// PendingActionStore reads a run and the decisions it waits on.
type PendingActionStore interface {
	LoadRun(ctx context.Context, runID string) (*core.RunRecord, error)
	ListPendingActionsByRun(ctx context.Context, runID string) ([]core.PendingActionRecord, error)
}

// OwnerNotifier records and sends an owner notification.
type OwnerNotifier interface {
	Notify(ctx context.Context, n core.Notification) (core.Notification, error)
}

// PendingActionAnnouncer pushes each decision an interrupted run waits on, so
// the owner can answer from the notification.
type PendingActionAnnouncer struct {
	store    PendingActionStore
	notifier OwnerNotifier
}

func NewPendingActionAnnouncer(store PendingActionStore, notifier OwnerNotifier) (*PendingActionAnnouncer, error) {
	if store == nil || notifier == nil {
		return nil, errors.New("pending action announcer: store and notifier are required")
	}
	return &PendingActionAnnouncer{store: store, notifier: notifier}, nil
}

// AnnounceRun notifies every pending action of runID. It runs after the run
// is marked interrupted, so a decision made from the notification resumes it.
func (a *PendingActionAnnouncer) AnnounceRun(ctx context.Context, runID string) error {
	run, err := a.store.LoadRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("announce pending actions of %s: %w", runID, err)
	}
	actions, err := a.store.ListPendingActionsByRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("announce pending actions of %s: %w", runID, err)
	}
	var errs []error
	for _, action := range actions {
		if action.Status != core.PendingActionStatusPending {
			continue
		}
		title, body, err := pendingActionText(action)
		if err != nil {
			errs = append(errs, fmt.Errorf("announce %s: %w", action.ActionID, err))
			continue
		}
		if _, err := a.notifier.Notify(ctx, core.Notification{
			Title: title, Body: body, ThreadID: run.SessionID, RunID: runID, ActionID: action.ActionID,
		}); err != nil {
			errs = append(errs, fmt.Errorf("announce %s: %w", action.ActionID, err))
		}
	}
	return errors.Join(errs...)
}

// pendingActionText is the stored title and body of an announcement: the
// subject, and the message or question the action carries.
func pendingActionText(action core.PendingActionRecord) (string, string, error) {
	title := strings.TrimSpace(action.Subject)
	if title == "" {
		title = string(action.Kind)
	}
	var payload struct {
		Message  string `json:"message"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(action.PayloadJSON), &payload); err != nil {
		return "", "", fmt.Errorf("decode payload: %w", err)
	}
	for _, body := range []string{payload.Message, payload.Question} {
		if body = strings.TrimSpace(body); body != "" {
			return title, body, nil
		}
	}
	return title, title, nil
}
