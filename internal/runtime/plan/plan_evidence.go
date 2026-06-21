package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
)

func latestEvidenceSummary(items []model.PlanEvidence) string {
	for i := len(items) - 1; i >= 0; i-- {
		summary := strings.TrimSpace(items[i].Summary)
		if summary != "" {
			return summary
		}
	}
	return ""
}

func validatePlanEvidence(stepID string, evidence model.PlanEvidence) error {
	if err := validateEvidenceIDs(stepID, evidence); err != nil {
		return err
	}
	if strings.TrimSpace(evidence.SourceRunID) == "" {
		return fmt.Errorf("plan evidence source_run_id is required")
	}
	if evidence.RecordedAt.IsZero() {
		return fmt.Errorf("plan evidence recorded_at is required")
	}
	if !validEvidenceKind(evidence.Kind) {
		return fmt.Errorf("plan evidence kind %q is invalid", evidence.Kind)
	}
	if !validEvidenceStatus(evidence.Status) {
		return fmt.Errorf("plan evidence status %q is invalid", evidence.Status)
	}
	if err := validateEvidenceKindRequirements(evidence); err != nil {
		return err
	}
	evidence.Command = trimmedNonEmptyStrings(evidence.Command)
	evidence.Paths = trimmedNonEmptyStrings(evidence.Paths)
	return nil
}

func validateEvidenceIDs(stepID string, evidence model.PlanEvidence) error {
	if strings.TrimSpace(stepID) == "" {
		return fmt.Errorf("plan step id is required")
	}
	if strings.TrimSpace(evidence.StepID) == "" {
		return fmt.Errorf("plan evidence step_id is required")
	}
	if strings.TrimSpace(evidence.StepID) != strings.TrimSpace(stepID) {
		return fmt.Errorf("plan evidence step_id %q does not match target step %q", evidence.StepID, stepID)
	}
	return nil
}

func validateEvidenceKindRequirements(evidence model.PlanEvidence) error {
	switch evidence.Kind {
	case model.EvidenceKindDiff:
		if strings.TrimSpace(evidence.DiffRef) == "" && len(trimmedNonEmptyStrings(evidence.Paths)) == 0 {
			return fmt.Errorf("plan evidence diff kind requires diff_ref or paths")
		}
	}
	return nil
}

func validEvidenceKind(kind model.EvidenceKind) bool {
	switch kind {
	case model.EvidenceKindTool, model.EvidenceKindCommand, model.EvidenceKindDiff, model.EvidenceKindCheckpoint, model.EvidenceKindRollback, model.EvidenceKindTest, model.EvidenceKindSubagent, model.EvidenceKindVerifier, model.EvidenceKindManual:
		return true
	default:
		return false
	}
}

func validEvidenceStatus(status model.EvidenceStatus) bool {
	switch status {
	case model.EvidenceStatusRecorded, model.EvidenceStatusPassed, model.EvidenceStatusFailed, model.EvidenceStatusConfirmed:
		return true
	default:
		return false
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

func ensureVerificationIntentCoverage(step model.PlanStep) error {
	if len(step.VerificationIntent) == 0 {
		return nil
	}
	missing := make([]string, 0, len(step.VerificationIntent))
	for _, intent := range step.VerificationIntent {
		if !intentCovered(intent, step.Evidence) {
			missing = append(missing, strings.TrimSpace(intent.Kind))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: step %s missing coverage for %s", ErrPlanStepVerificationGap, strings.TrimSpace(step.ID), strings.Join(missing, ", "))
}

func intentCovered(intent model.VerificationIntent, evidence []model.PlanEvidence) bool {
	kind := strings.TrimSpace(intent.Kind)
	for _, item := range evidence {
		if !evidenceCountsForCoverage(item) {
			continue
		}
		if intentMatchesKind(item, kind, intent) {
			return true
		}
	}
	return false
}

func intentMatchesKind(item model.PlanEvidence, kind string, intent model.VerificationIntent) bool {
	switch kind {
	case "read":
		return intentReadCovered(item)
	case "test":
		return item.Kind == model.EvidenceKindTest || (item.Kind == model.EvidenceKindCommand && commandMatchesIntent(item, intent))
	case "build", "lint":
		return item.Kind == model.EvidenceKindCommand && commandMatchesIntent(item, intent)
	case "diff":
		return item.Kind == model.EvidenceKindDiff
	case "checkpoint":
		return item.Kind == model.EvidenceKindCheckpoint
	case "rollback":
		return item.Kind == model.EvidenceKindRollback
	case "manual":
		return item.Kind == model.EvidenceKindManual && item.Status == model.EvidenceStatusConfirmed
	case "subagent":
		return item.Kind == model.EvidenceKindSubagent
	case "verifier":
		return item.Kind == model.EvidenceKindVerifier
	default:
		return false
	}
}

func intentReadCovered(item model.PlanEvidence) bool {
	return (item.Kind == model.EvidenceKindTool && isReadTool(item.ToolName)) ||
		(item.Kind == model.EvidenceKindManual && item.Status == model.EvidenceStatusConfirmed)
}

func evidenceCountsForCoverage(item model.PlanEvidence) bool {
	return item.Status == model.EvidenceStatusPassed || item.Status == model.EvidenceStatusConfirmed
}

func commandMatchesIntent(item model.PlanEvidence, intent model.VerificationIntent) bool {
	intentCommand := trimmedNonEmptyStrings(intent.Command)
	itemCommand := trimmedNonEmptyStrings(item.Command)
	if len(intentCommand) > 0 && !slices.Equal(intentCommand, itemCommand) {
		return false
	}
	intentPaths := trimmedNonEmptyStrings(intent.Paths)
	if len(intentPaths) == 0 {
		return true
	}
	itemPaths := trimmedNonEmptyStrings(item.Paths)
	if len(itemPaths) == 0 {
		return false
	}
	for _, path := range intentPaths {
		if slices.Contains(itemPaths, path) {
			return true
		}
	}
	return false
}

func isReadTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "read_file", "list_files", "search_text", "inspect_git_status", "inspect_git_diff", "git_summary":
		return true
	default:
		return false
	}
}

func delegatedSubagentEvidence(input toolMessageEvidenceInput) (*model.PlanEvidence, error) {
	if input.ToolName != "delegate_task" {
		return nil, nil
	}
	payload, err := parseDelegateTaskResult(input)
	if err != nil {
		return nil, err
	}
	status, errorText, summary := delegateAcceptanceOutcome(payload)
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("%s-subagent-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        model.EvidenceKindSubagent,
		Status:      status,
		Summary:     summary,
		ToolName:    input.ToolName,
		ChildRunID:  strings.TrimSpace(payload.ChildRunID),
		Error:       errorText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

func parseDelegateTaskResult(input toolMessageEvidenceInput) (orchestration.ChildAgentResult, error) {
	var payload orchestration.ChildAgentResult
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return orchestration.ChildAgentResult{}, fmt.Errorf("parse delegate_task result: %w", err)
	}
	if strings.TrimSpace(payload.ChildRunID) == "" {
		return orchestration.ChildAgentResult{}, fmt.Errorf("parse delegate_task result: child_run_id is required")
	}
	if strings.TrimSpace(payload.Acceptance.Status) == "" {
		return orchestration.ChildAgentResult{}, fmt.Errorf("parse delegate_task result: acceptance.status is required")
	}
	return payload, nil
}

func delegateAcceptanceOutcome(payload orchestration.ChildAgentResult) (model.EvidenceStatus, string, string) {
	childRunID := strings.TrimSpace(payload.ChildRunID)
	status := model.EvidenceStatusPassed
	errorText := ""
	summary := fmt.Sprintf("child run %s passed acceptance", childRunID)
	if strings.TrimSpace(payload.Acceptance.Status) != "passed" {
		status = model.EvidenceStatusFailed
		errorText = strings.Join(trimmedNonEmptyStrings(payload.Acceptance.Reasons), "; ")
		summary = fmt.Sprintf("child run %s failed acceptance", childRunID)
		if errorText == "" {
			errorText = fmt.Sprintf("child run %s acceptance failed", childRunID)
		}
	}
	return status, errorText, summary
}

func recorderFromMessageExtra(extra map[string]any) toolExecutionRecorder {
	if len(extra) == 0 {
		return toolExecutionRecorder{}
	}
	raw, ok := extra["plan_evidence_recorder"]
	if !ok {
		return toolExecutionRecorder{}
	}
	recorder, ok := raw.(toolExecutionRecorder)
	if ok {
		return recorder
	}
	ptr, ok := raw.(*toolExecutionRecorder)
	if ok && ptr != nil {
		return *ptr
	}
	return toolExecutionRecorder{}
}

func toolResultRefFromMessage(extra map[string]any) string {
	if len(extra) == 0 {
		return ""
	}
	raw, ok := extra["tool_result_ref"]
	if !ok {
		return ""
	}
	if value, ok := raw.(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func toolErrorReason(msg *planToolMessage) string {
	if msg == nil || msg.Extra == nil {
		return ""
	}
	if !toolErrorFlagged(msg.Extra) {
		return ""
	}
	reason := toolErrorReasonText(msg.Extra)
	if strings.TrimSpace(reason) != "" {
		return reason
	}
	return strings.TrimSpace(msg.Content)
}

func toolErrorFlagged(extra map[string]any) bool {
	rawFailed, ok := extra["tool_error"]
	if !ok {
		return false
	}
	value, valueOK := rawFailed.(bool)
	return valueOK && value
}

func toolErrorReasonText(extra map[string]any) string {
	rawReason, ok := extra["tool_error_reason"]
	if !ok {
		return ""
	}
	value, valueOK := rawReason.(string)
	if !valueOK {
		return ""
	}
	return value
}

func intentKinds(items []model.VerificationIntent, want ...string) bool {
	for _, item := range items {
		for _, candidate := range want {
			if strings.TrimSpace(item.Kind) == candidate {
				return true
			}
		}
	}
	return false
}

func intentPathsForKinds(items []model.VerificationIntent, kinds ...string) []string {
	out := make([]string, 0)
	for _, item := range items {
		for _, kind := range kinds {
			if strings.TrimSpace(item.Kind) != kind {
				continue
			}
			out = append(out, trimmedNonEmptyStrings(item.Paths)...)
		}
	}
	return trimmedNonEmptyStrings(out)
}
