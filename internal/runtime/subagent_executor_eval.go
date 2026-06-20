package runtime

import (
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/orchestration"
)

func evaluateDelegationAcceptance(
	req orchestration.ChildAgentRequest,
	runStatus events.RunStatus,
	outputSummary string,
	evidenceSummaries []string,
	planFailureReasons []string,
) orchestration.ChildAgentAcceptance {
	reasons := buildDelegationFailureReasons(req, runStatus, outputSummary, evidenceSummaries, planFailureReasons)
	if len(reasons) == 0 {
		return orchestration.ChildAgentAcceptance{Status: "passed"}
	}
	return orchestration.ChildAgentAcceptance{Status: "failed", Reasons: reasons}
}

func buildDelegationFailureReasons(req orchestration.ChildAgentRequest, runStatus events.RunStatus, outputSummary string, evidenceSummaries []string, planFailureReasons []string) []string {
	reasons := make([]string, 0, len(req.AcceptanceCriteria)+len(req.ExpectedEvidence)+len(planFailureReasons)+1)
	if runStatus != events.RunStatusSucceeded {
		reasons = append(reasons, fmt.Sprintf("child run finished with status %s", runStatus))
	}
	reasons = append(reasons, planFailureReasons...)
	for _, expected := range req.ExpectedEvidence {
		if !summaryContains(evidenceSummaries, expected) {
			reasons = append(reasons, fmt.Sprintf("missing expected evidence: %s", expected))
		}
	}
	for _, criterion := range req.AcceptanceCriteria {
		if !acceptanceCriterionSatisfied(criterion, outputSummary, evidenceSummaries) {
			reasons = append(reasons, fmt.Sprintf("acceptance criterion not satisfied: %s", criterion))
		}
	}
	return reasons
}

func acceptanceCriterionSatisfied(criterion string, outputSummary string, evidenceSummaries []string) bool {
	expected := strings.TrimSpace(criterion)
	if expected == "" {
		return true
	}
	if strings.Contains(strings.ToLower(outputSummary), strings.ToLower(expected)) {
		return true
	}
	return summaryContains(evidenceSummaries, expected)
}

func summaryContains(summaries []string, expected string) bool {
	target := strings.ToLower(strings.TrimSpace(expected))
	if target == "" {
		return true
	}
	for _, summary := range summaries {
		if strings.Contains(strings.ToLower(strings.TrimSpace(summary)), target) {
			return true
		}
	}
	return false
}

func normalizeToolNames(requested []string) []string {
	seen := make(map[string]struct{}, len(requested))
	result := make([]string, 0, len(requested))
	for _, name := range requested {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func normalizeChildAgentRequest(req orchestration.ChildAgentRequest) orchestration.ChildAgentRequest {
	req.ParentRunID = strings.TrimSpace(req.ParentRunID)
	req.ParentSessionID = strings.TrimSpace(req.ParentSessionID)
	req.ParentStepID = strings.TrimSpace(req.ParentStepID)
	req.Task = strings.TrimSpace(req.Task)
	req.AllowedToolNames = normalizeToolNames(req.AllowedToolNames)
	req.AcceptanceCriteria = normalizeToolNames(req.AcceptanceCriteria)
	req.ExpectedEvidence = normalizeToolNames(req.ExpectedEvidence)
	req.ChildRunMode = orchestration.NormalizeChildRunMode(req.ChildRunMode)
	req.WorkspaceMode = orchestration.NormalizeChildWorkspaceMode(req.WorkspaceMode)
	req.RequestedMode = events.OrchestrationMode(req.RequestedMode).Normalize()
	return req
}

func truncateTaskTitle(task string) string {
	title := strings.TrimSpace(task)
	if len(title) <= 80 {
		return title
	}
	return title[:77] + "..."
}
