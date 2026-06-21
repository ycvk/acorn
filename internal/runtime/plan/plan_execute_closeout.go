package plan

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/runtime/graph"
)

func NewCloseoutNode() *CloseoutNode {
	return &CloseoutNode{}
}

func (n *CloseoutNode) Invoke(ctx context.Context, state *graph.AgentGraphState) (*schema.Message, error) {
	if state == nil || state.Plan == nil || len(state.Plan.Steps) == 0 {
		return finalMessageFromGraphState(state), nil
	}
	completed, failed := closeoutStepSummaries(state.Plan.Steps)
	return closeoutSummaryMessage(completed, failed)
}

func closeoutStepSummaries(steps []model.PlanStep) ([]string, []string) {
	completed := make([]string, 0, len(steps))
	failed := make([]string, 0, len(steps))
	for _, step := range steps {
		switch step.Status {
		case model.PlanStepCompleted:
			completed = append(completed, closeoutStepLine(step, latestEvidenceSummary(step.Evidence)))
		case model.PlanStepFailed:
			failed = append(failed, closeoutFailedLine(step))
		}
	}
	return completed, failed
}

func closeoutStepLine(step model.PlanStep, summary string) string {
	if summary != "" {
		return summary
	}
	return step.Action
}

func closeoutFailedLine(step model.PlanStep) string {
	if reason, ok := failedPlanExecutionEvidenceReason(step.Evidence); ok && strings.TrimSpace(reason) != "" {
		return fmt.Sprintf("%s: %s", step.Action, reason)
	}
	return step.Action
}

func closeoutSummaryMessage(completed, failed []string) (*schema.Message, error) {
	if len(failed) == 0 && len(completed) == 1 {
		return schema.AssistantMessage(completed[0], nil), nil
	}
	var b strings.Builder
	b.WriteString(closeoutHeader(completed, failed))
	writeCloseoutSection(&b, "Completed:", completed)
	writeCloseoutSection(&b, "Not completed:", failed)
	return schema.AssistantMessage(strings.TrimSpace(b.String()), nil), nil
}

func closeoutHeader(completed, failed []string) string {
	switch {
	case len(failed) == 0:
		return "Completed the requested work."
	case len(completed) == 0:
		return "I could not complete the requested work."
	default:
		return "Completed part of the requested work, but not everything."
	}
}

func writeCloseoutSection(b *strings.Builder, header string, lines []string) {
	if len(lines) == 0 {
		return
	}
	b.WriteString("\n\n")
	b.WriteString(header)
	for _, line := range lines {
		b.WriteString("\n- ")
		b.WriteString(strings.TrimSpace(line))
	}
}
