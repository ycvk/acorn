package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/orchestration"
)

type DelegationSpec struct {
	Task               string   `json:"task" jsonschema:"description=The task to delegate to the subagent"`
	Context            string   `json:"context,omitempty" jsonschema:"description=Additional context for the subagent"`
	AllowedTools       []string `json:"allowed_tools,omitempty" jsonschema:"description=Optional allowlist of tool names the child run may use."`
	AcceptanceCriteria []string `json:"acceptance_criteria" jsonschema:"description=Required acceptance criteria the child run must satisfy before the parent step can complete."`
	ExpectedEvidence   []string `json:"expected_evidence,omitempty" jsonschema:"description=Optional evidence summaries that the child run is expected to produce."`
	WorkspaceMode      string   `json:"workspace_mode,omitempty" jsonschema:"description=Workspace isolation mode for the child run."`
}

type DelegateTaskInput = DelegationSpec

type DelegateTaskContext interface {
	CurrentRunID(ctx context.Context) string
	CurrentSessionID(ctx context.Context) string
}

func NewDelegateTool(exec orchestration.ChildAgentExecutor, bridge DelegateTaskContext) (einotool.BaseTool, error) {
	if exec == nil {
		return nil, errors.New("delegate_task requires child executor")
	}
	return toolutils.InferTool("delegate_task", "Delegate a subtask to a subagent for parallel or nested execution.", func(ctx context.Context, input DelegateTaskInput) (string, error) {
		spec := normalizedDelegationSpec(input)
		if strings.TrimSpace(spec.Task) == "" {
			return "", errors.New("task is required")
		}
		if len(spec.AcceptanceCriteria) == 0 {
			return "", errors.New("acceptance_criteria is required")
		}

		parentRunID := ""
		if bridge != nil {
			parentRunID = strings.TrimSpace(bridge.CurrentRunID(ctx))
		}
		if strings.TrimSpace(parentRunID) == "" {
			return "", errors.New("no active run context for delegation")
		}
		parentSessionID := ""
		if bridge != nil {
			parentSessionID = strings.TrimSpace(bridge.CurrentSessionID(ctx))
		}

		var messages []*schema.Message
		if strings.TrimSpace(spec.Context) != "" {
			messages = append(messages, schema.UserMessage(spec.Context))
		}

		result, err := exec.Execute(ctx, orchestration.ChildAgentRequest{
			ParentRunID:        parentRunID,
			ParentSessionID:    parentSessionID,
			Task:               spec.Task,
			WorkspaceMode:      orchestration.NormalizeChildWorkspaceMode(orchestration.ChildWorkspaceMode(spec.WorkspaceMode)),
			ContextMessages:    messages,
			AllowedToolNames:   append([]string(nil), spec.AllowedTools...),
			AcceptanceCriteria: append([]string(nil), spec.AcceptanceCriteria...),
			ExpectedEvidence:   append([]string(nil), spec.ExpectedEvidence...),
			Origin:             orchestration.ChildAgentOriginDelegateTask,
		})
		if err != nil {
			return "", fmt.Errorf("delegate task: %w", err)
		}
		body, err := result.MarshalJSON()
		if err != nil {
			return "", fmt.Errorf("delegate task: marshal child result: %w", err)
		}
		return string(body), nil
	})
}

func normalizedDelegationSpec(input DelegationSpec) DelegationSpec {
	return DelegationSpec{
		Task:               strings.TrimSpace(input.Task),
		Context:            strings.TrimSpace(input.Context),
		AllowedTools:       trimmedNonEmptyStrings(input.AllowedTools),
		AcceptanceCriteria: trimmedNonEmptyStrings(input.AcceptanceCriteria),
		ExpectedEvidence:   trimmedNonEmptyStrings(input.ExpectedEvidence),
		WorkspaceMode:      strings.TrimSpace(input.WorkspaceMode),
	}
}

func trimmedNonEmptyStrings(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
