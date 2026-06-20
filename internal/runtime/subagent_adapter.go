package runtime

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
)

type subagentExecutorAdapter struct {
	exec orchestration.ChildAgentExecutor
}

func (a subagentExecutorAdapter) ExecuteMessages(ctx context.Context, messages []*schema.Message) (string, error) {
	parentRunID := runtimeapi.GetRunID(ctx)
	if strings.TrimSpace(parentRunID) == "" {
		parentRunID = "sampling_parent"
	}
	task := "MCP sampling request"
	if len(messages) > 0 {
		task = messages[len(messages)-1].Content
	}
	result, err := a.exec.Execute(ctx, orchestration.ChildAgentRequest{
		ParentRunID:        parentRunID,
		Task:               task,
		ContextMessages:    append([]*schema.Message(nil), messages...),
		AcceptanceCriteria: []string{"sampling request completed"},
		Origin:             orchestration.ChildAgentOriginMCPSampling,
	})
	if err != nil {
		return "", err
	}
	return result.OutputSummary, nil
}
