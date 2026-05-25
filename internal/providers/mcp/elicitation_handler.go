package mcpprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/runtime/stream"
	"github.com/ycvk/acorn/internal/store"
)

const defaultElicitationTimeout = 30 * time.Second

// PendingActionStore is the pending-action persistence port required by MCP elicitation.
type PendingActionStore interface {
	CreatePendingAction(ctx context.Context, input store.CreatePendingActionInput) (*events.PendingActionRecord, error)
	LoadPendingAction(ctx context.Context, actionID string) (*events.PendingActionRecord, error)
	DecidePendingAction(ctx context.Context, actionID string, status events.PendingActionStatus, mode events.PendingActionDecisionMode, decisionJSON string) (*events.PendingActionRecord, error)
	SyncDecisionMessageForPendingAction(ctx context.Context, actionID string) error
	AppendEventContext(ctx context.Context, runID, kind string, payload any) (events.EventRecord, error)
}

// ElicitationHandler handles MCP server elicitation/create requests by creating
// a PendingAction, emitting stream items, and blocking until the operator decides
// or the 30s timeout expires (returning "decline" on timeout).
//
// Per RESEARCH.md Pitfall 1, this uses a channel-wait-with-timeout pattern
// instead of StatefulInterrupt, because MCP client callbacks must return a
// result synchronously to the server.
type ElicitationHandler struct {
	store       PendingActionStore
	onEvent     ProviderEventCallback
	activeRunID string
	activeMu    sync.RWMutex
	timeout     time.Duration
}

func newElicitationHandler(store PendingActionStore, onEvent ProviderEventCallback) *ElicitationHandler {
	return &ElicitationHandler{
		store:   store,
		onEvent: onEvent,
		timeout: defaultElicitationTimeout,
	}
}

func (h *ElicitationHandler) setActiveRunID(runID string) {
	h.activeMu.Lock()
	defer h.activeMu.Unlock()
	h.activeRunID = strings.TrimSpace(runID)
}

func (h *ElicitationHandler) getActiveRunID() string {
	h.activeMu.RLock()
	defer h.activeMu.RUnlock()
	return h.activeRunID
}

// HandleElicitation handles an MCP elicitation/create request.
// Per D-01 (reuse PendingAction), D-02 (30s timeout), D-03 (always require approval):
//
//  1. If no active run context, return decline immediately.
//  2. Create a PendingAction with Kind="elicitation" in the store.
//  3. Emit an elicitation.pending event.
//  4. Poll the store every 500ms until the action is decided or timeout.
//  5. On timeout, return ElicitResult{Action: "decline"}.
//  6. On decision, map the PendingAction status to the ElicitResult action.
//  7. Emit an elicitation.decided event.
//  8. Return the ElicitResult to the MCP server.
func (h *ElicitationHandler) HandleElicitation(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	runID := h.getActiveRunID()
	if strings.TrimSpace(runID) == "" {
		return &mcp.ElicitResult{Action: "decline"}, nil
	}

	actionID := newElicitationActionID()

	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return nil, fmt.Errorf("marshal elicitation params: %w", err)
	}

	record, err := h.store.CreatePendingAction(ctx, store.CreatePendingActionInput{
		ActionID:    actionID,
		RunID:       runID,
		Kind:        events.PendingActionKindElicitation,
		Subject:     "elicitation",
		PayloadJSON: string(paramsJSON),
		Status:      events.PendingActionStatusPending,
		Mode:        events.PendingActionModeDeferred,
	})
	if err != nil {
		return nil, fmt.Errorf("create elicitation pending action: %w", err)
	}
	if err := h.store.SyncDecisionMessageForPendingAction(ctx, record.ActionID); err != nil {
		return nil, fmt.Errorf("sync elicitation pending action message: %w", err)
	}

	if err := h.emitElicitationEvent(ctx, runID, record.ActionID, req.Params, string(stream.StreamKindElicitationPending)); err != nil {
		return nil, err
	}

	// Poll until decided or timeout
	result, err := h.waitForDecision(ctx, actionID, h.timeout)
	if err != nil {
		return nil, err
	}
	if err := h.store.SyncDecisionMessageForPendingAction(ctx, record.ActionID); err != nil {
		return nil, fmt.Errorf("sync elicitation decided message: %w", err)
	}

	if err := h.emitElicitationEvent(ctx, runID, record.ActionID, req.Params, string(stream.StreamKindElicitationDecided)); err != nil {
		return nil, err
	}

	return result, nil
}

// waitForDecision polls the store for the PendingAction status until it changes
// from "pending" or the timeout expires. Returns decline on timeout.
func (h *ElicitationHandler) waitForDecision(ctx context.Context, actionID string, timeout time.Duration) (*mcp.ElicitResult, error) {
	deadline := time.Now().Add(timeout)
	pollInterval := 500 * time.Millisecond

	for {
		action, err := h.store.LoadPendingAction(ctx, actionID)
		if err != nil {
			return nil, fmt.Errorf("load elicitation pending action %s: %w", actionID, err)
		}

		if action.Status != events.PendingActionStatusPending {
			return pendingActionStatusToElicitResult(action.Status), nil
		}

		if time.Now().After(deadline) {
			// Timeout: auto-decline per D-02
			slog.Info("elicitation handler: timed out waiting for operator decision, auto-declining",
				"action_id", actionID)
			decisionJSON, err := json.Marshal(map[string]any{
				"action": "decline",
				"reason": "timeout",
			})
			if err != nil {
				return nil, fmt.Errorf("marshal elicitation timeout decision: %w", err)
			}
			if _, err := h.store.DecidePendingAction(ctx, actionID, events.PendingActionStatusRejected, events.PendingActionModeDeferred, string(decisionJSON)); err != nil {
				return nil, err
			}
			return &mcp.ElicitResult{Action: "decline"}, nil
		}

		select {
		case <-ctx.Done():
			return &mcp.ElicitResult{Action: "decline"}, nil
		case <-time.After(pollInterval):
			// Continue polling
		}
	}
}

// pendingActionStatusToElicitResult maps PendingAction status to ElicitResult action.
func pendingActionStatusToElicitResult(status events.PendingActionStatus) *mcp.ElicitResult {
	switch status {
	case events.PendingActionStatusApproved:
		return &mcp.ElicitResult{Action: "accept"}
	case events.PendingActionStatusRejected:
		return &mcp.ElicitResult{Action: "decline"}
	default:
		return &mcp.ElicitResult{Action: "decline"}
	}
}

// emitElicitationEvent emits an elicitation event via the store's AppendEventContext.
func (h *ElicitationHandler) emitElicitationEvent(ctx context.Context, runID, actionID string, params *mcp.ElicitParams, eventKind string) error {
	if h.store == nil {
		return fmt.Errorf("elicitation event store not configured")
	}
	payload := stream.ElicitationPayload{
		ActionID: actionID,
	}
	if params != nil {
		payload.Message = params.Message
		payload.RequestedSchema = params.RequestedSchema
	}

	_, err := h.store.AppendEventContext(ctx, runID, eventKind, payload)
	if err != nil {
		return fmt.Errorf("append elicitation event %s: %w", eventKind, err)
	}
	return nil
}

func newElicitationActionID() string {
	return fmt.Sprintf("action_%d", time.Now().UTC().UnixNano())
}
