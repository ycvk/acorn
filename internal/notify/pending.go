package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// PendingActionStore reads a run, the decisions it waits on and when the
// owner last wrote in a conversation (the zero time when they never did).
type PendingActionStore interface {
	LoadRun(ctx context.Context, runID string) (*core.RunRecord, error)
	ListPendingActionsByRun(ctx context.Context, runID string) ([]core.PendingActionRecord, error)
	LatestOwnerMessageAt(ctx context.Context, sessionID string) (time.Time, error)
}

// OwnerNotifier records and sends an owner notification. NotifyAwaited sends
// one the owner is waiting for, which quiet hours do not hold.
type OwnerNotifier interface {
	Notify(ctx context.Context, n core.Notification) (core.Notification, error)
	NotifyAwaited(ctx context.Context, n core.Notification) (core.Notification, error)
}

// awaitedWindow is how long after their last message in a conversation the
// owner counts as waiting for what a run of it asks them.
const awaitedWindow = 30 * time.Minute

// PendingActionAnnouncer pushes each decision an interrupted run waits on, so
// the owner can answer from the notification. A decision asked within
// awaitedWindow of the owner's last message in the conversation is pushed
// inside quiet hours too: the owner just asked for the work it holds up.
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
	ownerWrote, err := a.store.LatestOwnerMessageAt(ctx, run.SessionID)
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
		n := core.Notification{Title: title, Body: body, ThreadID: run.SessionID, RunID: runID, ActionID: action.ActionID}
		send := a.notifier.Notify
		if !ownerWrote.IsZero() && action.CreatedAt.Sub(ownerWrote) <= awaitedWindow {
			send = a.notifier.NotifyAwaited
		}
		if _, err := send(ctx, n); err != nil {
			errs = append(errs, fmt.Errorf("announce %s: %w", action.ActionID, err))
		}
	}
	return errors.Join(errs...)
}

// pendingActionText is the title and body recorded for an announcement: the
// subject, and the message or question the action carries. The push sends
// the title; the app reads the rest from the action.
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
