package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/cloudwego/eino/adk"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/review"
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
	LoadRun(ctx context.Context, runID string) (*core.RunRecord, error)
}

// ActionReviewer judges whether a call needs the owner's approval.
type ActionReviewer interface {
	Review(ctx context.Context, call review.Call) (review.Verdict, error)
}

// approvalMiddleware gates tool calls by the owner's rules and, for calls the
// rules leave open, by review. A gated call records a tool_approval pending
// action and interrupts; on resume an accepted call runs with the recorded
// arguments and a declined call returns a notice to the model instead.
type approvalMiddleware struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]
	rules    config.ApprovalConfig
	reviewer ActionReviewer
	store    approvalStore

	mu    sync.Mutex
	pages map[pageKey]review.Page
}

// pageKey names a bsk session.
type pageKey struct {
	session string
}

// approvalRequest is why a call waits for the owner and how to show it.
type approvalRequest struct {
	reason  string
	summary string
	verdict *review.Verdict
}

// newApprovalMiddleware builds the gate for one run. A nil reviewer leaves
// calls no rule settles to run unreviewed.
func newApprovalMiddleware(rules config.ApprovalConfig, reviewer ActionReviewer, store approvalStore) (*approvalMiddleware, error) {
	if store == nil {
		return nil, errors.New("approval middleware requires a store")
	}
	for _, patterns := range [][]string{rules.Require, rules.Block, rules.Allow} {
		for i, p := range patterns {
			if _, err := path.Match(p, ""); err != nil {
				return nil, fmt.Errorf("approval pattern [%d] %q: %w", i, p, err)
			}
		}
	}
	return &approvalMiddleware{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.AgenticMessage]{},
		rules: config.ApprovalConfig{
			Require: slices.Clone(rules.Require),
			Block:   slices.Clone(rules.Block),
			Allow:   slices.Clone(rules.Allow),
		},
		reviewer: reviewer,
		store:    store,
		pages:    map[pageKey]review.Page{},
	}, nil
}

// matchesAny takes patterns newApprovalMiddleware already validated, so
// path.Match cannot fail on them.
func matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, err := path.Match(p, name); err == nil && ok {
			return true
		}
	}
	return false
}

// reviewed are the tools whose effects can leave Acorn and the workstation.
func reviewed(name string) bool {
	return name == "shell" || strings.HasPrefix(name, "mcp__")
}

func (m *approvalMiddleware) WrapInvokableToolCall(_ context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	run := endpoint
	if tCtx.Name == "shell" {
		run = m.observing(endpoint)
	}
	switch {
	case matchesAny(m.rules.Block, tCtx.Name):
		return func(context.Context, string, ...einotool.Option) (string, error) {
			return fmt.Sprintf("The owner's rules block %s (approval.block). Do not retry it; tell the owner what you wanted to do.", tCtx.Name), nil
		}, nil
	case matchesAny(m.rules.Require, tCtx.Name):
		return m.gated(run, tCtx, func(context.Context, string) (*approvalRequest, error) {
			return &approvalRequest{reason: "approval.require"}, nil
		}), nil
	case matchesAny(m.rules.Allow, tCtx.Name) || m.reviewer == nil || !reviewed(tCtx.Name):
		return run, nil
	default:
		return m.gated(run, tCtx, m.review(tCtx.Name)), nil
	}
}

// gated runs check on the first execution; a non-nil request interrupts for
// the owner's decision, nil runs the call.
func (m *approvalMiddleware) gated(endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext, check func(context.Context, string) (*approvalRequest, error)) adk.InvokableToolCallEndpoint {
	return func(ctx context.Context, args string, opts ...einotool.Option) (string, error) {
		wasInterrupted, hasState, state := einotool.GetInterruptState[toolApprovalState](ctx)
		if !wasInterrupted {
			request, err := check(ctx, args)
			if err != nil {
				return "", err
			}
			if request == nil {
				return endpoint(ctx, args, opts...)
			}
			return "", m.requestApproval(ctx, tCtx, args, *request)
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
	}
}

// review returns the check for a reviewed tool. A failed review needs the
// owner's approval and says so.
func (m *approvalMiddleware) review(toolName string) func(context.Context, string) (*approvalRequest, error) {
	return func(ctx context.Context, args string) (*approvalRequest, error) {
		calls, err := m.reviewCalls(ctx, toolName, args)
		if err != nil || len(calls) == 0 {
			return nil, err
		}
		verdicts := make([]review.Verdict, len(calls))
		errs := make([]error, len(calls))
		started := time.Now()
		var wg sync.WaitGroup
		for i, call := range calls {
			wg.Go(func() { verdicts[i], errs[i] = m.reviewer.Review(ctx, call) })
		}
		wg.Wait()
		var request *approvalRequest
		for i, call := range calls {
			v := verdicts[i]
			if errs[i] != nil {
				v = review.Verdict{NeedsApproval: true, Reasons: []string{"审查服务不可用"}}
			}
			if err := m.recordReview(ctx, toolName, call, v, errs[i], time.Since(started)); err != nil {
				return nil, err
			}
			if v.NeedsApproval && request == nil {
				request = &approvalRequest{
					reason:  "review",
					summary: review.Describe(call) + " —— " + strings.Join(v.Reasons, "、"),
					verdict: &v,
				}
			}
		}
		return request, nil
	}
}

// reviewCalls turns a tool call into what review judges. Read-only shell
// commands need none; each bsk page action is judged with the page the
// agent last observed, and any other part as the whole command.
func (m *approvalMiddleware) reviewCalls(ctx context.Context, toolName, args string) ([]review.Call, error) {
	owner, err := m.ownerRequest(ctx)
	if err != nil {
		return nil, err
	}
	if toolName != "shell" {
		return []review.Call{{OwnerRequest: owner, Tool: toolName, Arguments: args}}, nil
	}
	command, ok := shellCommand(args)
	if !ok {
		// The shell tool rejects the same arguments, so the call fails as a
		// tool result without running a command.
		return nil, nil
	}
	shell := review.ParseShell(command)
	if shell.ReadOnly {
		return nil, nil
	}
	var calls []review.Call
	m.mu.Lock()
	for _, a := range shell.Actions() {
		page := m.pages[pageKey{a.Session}]
		action := a.Action
		value := a.Value
		if action == "press" {
			action, value = "press "+a.Value, ""
		}
		target := page.Refs[a.Ref]
		if target == "" && a.Ref != "" && !strings.HasPrefix(a.Ref, "@e") {
			target = "CSS selector " + a.Ref
		}
		calls = append(calls, review.Call{OwnerRequest: owner, Browser: &review.BrowserAction{
			Action: action, Value: value, Target: target, URL: page.URL, Title: page.Title,
		}})
	}
	m.mu.Unlock()
	if shell.Other {
		calls = append(calls, review.Call{OwnerRequest: owner, Tool: "shell", Command: command})
	}
	return calls, nil
}

// shellCommand reads the command from shell arguments; ok is false when they
// do not decode, which the shell tool rejects too.
func shellCommand(args string) (command string, ok bool) {
	var input struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(args), &input) != nil {
		return "", false
	}
	return input.Command, true
}

// observing remembers what bsk showed of each session, so a later click on
// @eN is reviewed with the element's name and the page it is on.
func (m *approvalMiddleware) observing(endpoint adk.InvokableToolCallEndpoint) adk.InvokableToolCallEndpoint {
	return func(ctx context.Context, args string, opts ...einotool.Option) (string, error) {
		out, err := endpoint(ctx, args, opts...)
		if err != nil {
			return out, err
		}
		// A failed call arrives as the tool error handler's text; it showed no page.
		if !json.Valid([]byte(out)) {
			return out, nil
		}
		var result struct {
			Stdout string `json:"stdout"`
		}
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			return "", fmt.Errorf("observe shell result: %w", err)
		}
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(args), &input); err != nil {
			return "", fmt.Errorf("observe shell arguments: %w", err)
		}
		sessions := map[pageKey]bool{}
		for _, c := range review.ParseShell(input.Command).Bsk {
			if c.Session != "" {
				sessions[pageKey{c.Session}] = true
			}
		}
		// Output mixing several sessions cannot be attributed.
		if len(sessions) == 1 {
			m.mu.Lock()
			for key := range sessions {
				m.pages[key] = m.pages[key].Merge(review.ObservePage(result.Stdout))
			}
			m.mu.Unlock()
		}
		return out, nil
	}
}

func (m *approvalMiddleware) ownerRequest(ctx context.Context) (string, error) {
	runID := core.CurrentRunID(ctx)
	if runID == "" {
		return "", errors.New("review requires a run id in context")
	}
	run, err := m.store.LoadRun(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("load run for review: %w", err)
	}
	return run.Input, nil
}

func (m *approvalMiddleware) recordReview(ctx context.Context, toolName string, call review.Call, v review.Verdict, reviewErr error, took time.Duration) error {
	payload := map[string]any{
		"tool_name":      toolName,
		"summary":        review.Describe(call),
		"needs_approval": v.NeedsApproval,
		"reasons":        v.Reasons,
		"scores":         v.Scores,
		"model":          v.Model,
		"duration_ms":    took.Milliseconds(),
		"state":          review.State(call),
	}
	if reviewErr != nil {
		payload["error"] = reviewErr.Error()
	}
	if _, err := m.store.AppendEvent(ctx, core.CurrentRunID(ctx), "tool_review", payload); err != nil {
		return fmt.Errorf("append tool_review event: %w", err)
	}
	return nil
}

func (m *approvalMiddleware) requestApproval(ctx context.Context, tCtx *adk.ToolContext, args string, request approvalRequest) error {
	runID := core.CurrentRunID(ctx)
	if runID == "" {
		return errors.New("tool approval requires a run id in context")
	}
	state := toolApprovalState{
		ActionID:  core.NewActionID(),
		ToolName:  tCtx.Name,
		Arguments: args,
	}
	payload := map[string]any{
		"message":   fmt.Sprintf("%s wants to run with arguments:\n%s", tCtx.Name, args),
		"tool_name": tCtx.Name,
		"arguments": args,
	}
	if request.summary != "" {
		payload["summary"] = request.summary
		payload["message"] = request.summary
	}
	if request.verdict != nil {
		payload["review"] = request.verdict
	}
	payloadJSON, err := json.Marshal(payload)
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
		Reason:      request.reason,
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
