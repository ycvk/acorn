package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"

	"github.com/cloudwego/eino/schema"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
)

const toolApprovalInterruptKind = "tool_approval"

// toolApprovalState is checkpointed with the interrupt so the resumed call can
// prove it executes exactly the arguments the owner approved.
type toolApprovalState struct {
	ActionID  string
	ToolName  string
	Arguments string
}

type approvalStore interface {
	CreatePendingAction(ctx context.Context, input core.PendingActionInput) (*core.PendingActionRecord, error)
	AppendEvent(ctx context.Context, runID, kind string, payload any) (core.EventRecord, error)
}

// approvalMiddleware pauses tool calls whose names match an approval pattern.
// The first execution records a tool_approval pending action and interrupts;
// on resume an accepted call runs with the recorded arguments and a declined
// call returns a notice to the model instead of executing.
type approvalMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	patterns []string
	store    approvalStore
}

func newApprovalMiddleware(patterns []string, store approvalStore) (*approvalMiddleware, error) {
	if store == nil {
		return nil, errors.New("approval middleware requires a store")
	}
	for i, p := range patterns {
		if _, err := path.Match(p, ""); err != nil {
			return nil, fmt.Errorf("approval pattern [%d] %q: %w", i, p, err)
		}
	}
	return &approvalMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		patterns:                          slices.Clone(patterns),
		store:                             store,
	}, nil
}

func (m *approvalMiddleware) requiresApproval(toolName string) (bool, error) {
	for _, p := range m.patterns {
		matched, err := path.Match(p, toolName)
		if err != nil {
			return false, fmt.Errorf("approval pattern %q: %w", p, err)
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func (m *approvalMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	required, err := m.requiresApproval(tCtx.Name)
	if err != nil || !required {
		return endpoint, err
	}
	return func(ctx context.Context, args string, opts ...einotool.Option) (string, error) {
		wasInterrupted, hasState, state := einotool.GetInterruptState[toolApprovalState](ctx)
		if !wasInterrupted {
			return "", m.requestApproval(ctx, tCtx, args)
		}
		if !hasState {
			return "", fmt.Errorf("tool approval resume for %s has no saved state", tCtx.Name)
		}
		if state.Arguments != args {
			return "", fmt.Errorf("tool approval %s: arguments changed between interrupt and resume", state.ActionID)
		}
		isTarget, hasData, data := einotool.GetResumeContext[map[string]any](ctx)
		if !isTarget {
			return "", einotool.StatefulInterrupt(ctx, approvalInterruptInfo(state), state)
		}
		if !hasData {
			return "", fmt.Errorf("tool approval %s resumed without a decision", state.ActionID)
		}
		decision, ok := data["action"].(string)
		if !ok {
			return "", fmt.Errorf("tool approval %s: decision has no action", state.ActionID)
		}
		switch decision {
		case "accept":
			return endpoint(ctx, args, opts...)
		case "decline":
			return fmt.Sprintf("The owner declined this %s call. Do not retry it; continue without it or ask the owner what to do instead.", tCtx.Name), nil
		default:
			return "", fmt.Errorf("tool approval %s: unsupported decision %q", state.ActionID, decision)
		}
	}, nil
}

func (m *approvalMiddleware) requestApproval(ctx context.Context, tCtx *adk.ToolContext, args string) error {
	runID := core.CurrentRunID(ctx)
	if runID == "" {
		return errors.New("tool approval requires a run id in context")
	}
	state := toolApprovalState{
		ActionID:  core.NewActionID(),
		ToolName:  tCtx.Name,
		Arguments: args,
	}
	payloadJSON, err := json.Marshal(map[string]any{
		"message":   fmt.Sprintf("%s wants to run with arguments:\n%s", tCtx.Name, args),
		"tool_name": tCtx.Name,
		"arguments": args,
	})
	if err != nil {
		return fmt.Errorf("marshal tool approval payload: %w", err)
	}
	if _, err := m.store.CreatePendingAction(ctx, core.PendingActionInput{
		ActionID:    state.ActionID,
		RunID:       runID,
		Kind:        core.PendingActionKindToolApproval,
		Subject:     "Approve " + tCtx.Name,
		PayloadJSON: string(payloadJSON),
		Status:      core.PendingActionStatusPending,
		Reason:      "approval.require",
	}); err != nil {
		return err
	}
	if _, err := m.store.AppendEvent(ctx, runID, "tool_approval.pending", map[string]any{
		"action_id": state.ActionID,
		"tool_name": tCtx.Name,
		"arguments": args,
	}); err != nil {
		return fmt.Errorf("append tool_approval.pending event: %w", err)
	}
	return einotool.StatefulInterrupt(ctx, approvalInterruptInfo(state), state)
}

func approvalInterruptInfo(state toolApprovalState) map[string]any {
	return map[string]any{
		"kind":      toolApprovalInterruptKind,
		"action_id": state.ActionID,
		"tool_name": state.ToolName,
	}
}
