package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/core"
)

const defaultElicitationTimeout = 30 * time.Second

// ElicitationHandler handles MCP server elicitation/create requests by creating
// a PendingAction, emitting stream items, and blocking until the operator decides
// or the 30s timeout expires (declining the action on timeout).
//
// It waits inside the callback instead of interrupting the run, because an MCP
// client callback must return its result to the server synchronously.
type ElicitationHandler struct {
	store       core.SessionStore
	activeRunID string
	activeMu    sync.RWMutex
	timeout     time.Duration
}

func newElicitationHandler(store core.SessionStore) *ElicitationHandler {
	return &ElicitationHandler{
		store:   store,
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

// HandleElicitation handles an MCP elicitation/create request. Every request
// waits for the owner's decision on a PendingAction:
//
//  1. If no active run context, return decline immediately.
//  2. A request for fields fails: the owner's app can accept or decline,
//     and has no form to fill them in.
//  3. Create a PendingAction with Kind="elicitation" in the store and emit an
//     elicitation.pending event.
//  4. Poll the store every 500ms until the action is decided.
//  5. Past the timeout, or when the call's context ends, decline the action
//     in the store and emit elicitation.decided; a decision the owner made
//     emitted its own.
//  6. Return the decision as the ElicitResult.
func (h *ElicitationHandler) HandleElicitation(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	runID := h.getActiveRunID()
	if strings.TrimSpace(runID) == "" {
		return &mcp.ElicitResult{Action: "decline"}, nil
	}
	fields, err := requestedFields(req.Params)
	if err != nil {
		return nil, err
	}
	if len(fields) > 0 {
		return nil, fmt.Errorf("the server asks the owner for %s, and the owner's app can only accept or decline", strings.Join(fields, ", "))
	}

	actionID := core.NewActionID()

	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return nil, fmt.Errorf("marshal elicitation params: %w", err)
	}

	record, err := h.store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID:    actionID,
		RunID:       runID,
		Kind:        core.PendingActionKindElicitation,
		Subject:     "elicitation",
		PayloadJSON: string(paramsJSON),
	})
	if err != nil {
		return nil, fmt.Errorf("create elicitation pending action: %w", err)
	}
	if err := h.emitElicitationEvent(ctx, runID, record.ActionID, req.Params, string(core.StreamKindElicitationPending)); err != nil {
		return nil, err
	}
	return h.waitForDecision(ctx, runID, record.ActionID, req.Params, h.timeout)
}

// requestedFields lists the properties a form elicitation's schema asks for.
func requestedFields(params *mcp.ElicitParams) ([]string, error) {
	if params == nil || params.RequestedSchema == nil {
		return nil, nil
	}
	raw, err := json.Marshal(params.RequestedSchema)
	if err != nil {
		return nil, fmt.Errorf("read elicitation schema: %w", err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("read elicitation schema: %w", err)
	}
	return slices.Sorted(maps.Keys(schema.Properties)), nil
}

// waitForDecision polls the store until the owner decides the action. Past
// the timeout, or when ctx ends, it declines the action itself, unless the
// owner's decision came first.
func (h *ElicitationHandler) waitForDecision(ctx context.Context, runID, actionID string, params *mcp.ElicitParams, timeout time.Duration) (*mcp.ElicitResult, error) {
	deadline := time.Now().Add(timeout)
	pollInterval := 500 * time.Millisecond

	for {
		action, err := h.store.LoadPendingAction(ctx, actionID)
		if err != nil {
			return nil, fmt.Errorf("load elicitation pending action %s: %w", actionID, err)
		}
		if action.Status != core.PendingActionStatusPending {
			return pendingActionStatusToElicitResult(action.Status), nil
		}
		if time.Now().After(deadline) {
			slog.Info("elicitation handler: timed out waiting for operator decision, auto-declining", "action_id", actionID)
			return h.decline(ctx, runID, actionID, params, "timeout")
		}
		select {
		case <-ctx.Done():
			return h.decline(context.WithoutCancel(ctx), runID, actionID, params, "the call ended")
		case <-time.After(pollInterval):
		}
	}
}

// decline records the handler's own decline of a pending action and its
// elicitation.decided event. When the owner decided first, theirs stands.
func (h *ElicitationHandler) decline(ctx context.Context, runID, actionID string, params *mcp.ElicitParams, reason string) (*mcp.ElicitResult, error) {
	decisionJSON, err := json.Marshal(map[string]any{"action": "decline", "reason": reason})
	if err != nil {
		return nil, fmt.Errorf("marshal elicitation decline: %w", err)
	}
	_, err = h.store.DecidePendingAction(ctx, actionID, core.PendingActionStatusRejected, string(decisionJSON))
	if errors.Is(err, core.ErrPendingActionDecided) {
		action, err := h.store.LoadPendingAction(ctx, actionID)
		if err != nil {
			return nil, fmt.Errorf("load elicitation pending action %s: %w", actionID, err)
		}
		return pendingActionStatusToElicitResult(action.Status), nil
	}
	if err != nil {
		return nil, err
	}
	if err := h.emitElicitationEvent(ctx, runID, actionID, params, string(core.StreamKindElicitationDecided)); err != nil {
		return nil, err
	}
	return &mcp.ElicitResult{Action: "decline"}, nil
}

// pendingActionStatusToElicitResult maps PendingAction status to ElicitResult action.
func pendingActionStatusToElicitResult(status core.PendingActionStatus) *mcp.ElicitResult {
	switch status {
	case core.PendingActionStatusApproved:
		return &mcp.ElicitResult{Action: "accept"}
	case core.PendingActionStatusRejected:
		return &mcp.ElicitResult{Action: "decline"}
	default:
		return &mcp.ElicitResult{Action: "decline"}
	}
}

// emitElicitationEvent emits an elicitation event via the store's AppendEvent.
func (h *ElicitationHandler) emitElicitationEvent(ctx context.Context, runID, actionID string, params *mcp.ElicitParams, eventKind string) error {
	payload := map[string]any{
		"action_id": actionID,
	}
	if params != nil {
		payload["message"] = params.Message
		payload["requested_schema"] = params.RequestedSchema
	}

	_, err := h.store.AppendEvent(ctx, runID, eventKind, payload)
	if err != nil {
		return fmt.Errorf("append elicitation event %s: %w", eventKind, err)
	}
	return nil
}
