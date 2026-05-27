package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/graph"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/tooling"
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
	if strings.TrimSpace(stepID) == "" {
		return fmt.Errorf("plan step id is required")
	}
	if strings.TrimSpace(evidence.StepID) == "" {
		return fmt.Errorf("plan evidence step_id is required")
	}
	if strings.TrimSpace(evidence.StepID) != strings.TrimSpace(stepID) {
		return fmt.Errorf("plan evidence step_id %q does not match target step %q", evidence.StepID, stepID)
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
	switch evidence.Kind {
	case model.EvidenceKindDiff:
		if strings.TrimSpace(evidence.DiffRef) == "" && len(trimmedNonEmptyStrings(evidence.Paths)) == 0 {
			return fmt.Errorf("plan evidence diff kind requires diff_ref or paths")
		}
	}
	evidence.Command = trimmedNonEmptyStrings(evidence.Command)
	evidence.Paths = trimmedNonEmptyStrings(evidence.Paths)
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
		switch kind {
		case "read":
			if (item.Kind == model.EvidenceKindTool && isReadTool(item.ToolName)) || (item.Kind == model.EvidenceKindManual && item.Status == model.EvidenceStatusConfirmed) {
				return true
			}
		case "test":
			if item.Kind == model.EvidenceKindTest {
				return true
			}
			if item.Kind == model.EvidenceKindCommand && commandMatchesIntent(item, intent) {
				return true
			}
		case "build", "lint":
			if item.Kind == model.EvidenceKindCommand && commandMatchesIntent(item, intent) {
				return true
			}
		case "diff":
			if item.Kind == model.EvidenceKindDiff {
				return true
			}
		case "checkpoint":
			if item.Kind == model.EvidenceKindCheckpoint {
				return true
			}
		case "rollback":
			if item.Kind == model.EvidenceKindRollback {
				return true
			}
		case "manual":
			if item.Kind == model.EvidenceKindManual && item.Status == model.EvidenceStatusConfirmed {
				return true
			}
		case "subagent":
			if item.Kind == model.EvidenceKindSubagent {
				return true
			}
		case "verifier":
			if item.Kind == model.EvidenceKindVerifier {
				return true
			}
		}
	}
	return false
}

func verifierEvidenceFromResult(stepID, parentRunID string, result *orchestration.VerificationResult, recordedAt time.Time) model.PlanEvidence {
	if result == nil {
		reason := "verifier result is nil"
		return model.PlanEvidence{
			ID:          fmt.Sprintf("verifier-%d", recordedAt.UnixNano()),
			StepID:      stepID,
			Kind:        model.EvidenceKindVerifier,
			Status:      model.EvidenceStatusRecorded,
			Summary:     reason,
			Error:       reason,
			SourceRunID: parentRunID,
			RecordedAt:  recordedAt,
		}
	}
	status := model.EvidenceStatusRecorded
	errText := ""
	switch result.Verdict {
	case orchestration.VerificationVerdictPassed:
		status = model.EvidenceStatusPassed
	case orchestration.VerificationVerdictFailed:
		status = model.EvidenceStatusFailed
		errText = strings.Join(trimmedNonEmptyStrings(result.BlockingFindings), "; ")
		if errText == "" {
			errText = "verifier failed"
		}
	case orchestration.VerificationVerdictInconclusive:
		errText = strings.Join(trimmedNonEmptyStrings(result.MissingEvidence), "; ")
		if errText == "" {
			errText = "verifier inconclusive"
		}
	default:
		errText = fmt.Sprintf("verifier verdict %q is inconclusive", strings.TrimSpace(string(result.Verdict)))
	}
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		summary = fmt.Sprintf("verifier verdict: %s", strings.TrimSpace(string(result.Verdict)))
	}
	return model.PlanEvidence{
		ID:          fmt.Sprintf("verifier-%d", recordedAt.UnixNano()),
		StepID:      stepID,
		Kind:        model.EvidenceKindVerifier,
		Status:      status,
		Summary:     summary,
		ChildRunID:  strings.TrimSpace(result.ChildRunID),
		Error:       errText,
		SourceRunID: parentRunID,
		RecordedAt:  recordedAt,
	}
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

type toolExecutionRecorder struct {
	items []recordedToolArtifact
}

type recordedToolArtifact struct {
	Kind    model.EvidenceKind
	Status  model.EvidenceStatus
	Summary string
	Paths   []string
	DiffRef string
	Error   string
}

type toolMessageEvidenceInput struct {
	Step          model.PlanStep
	RunID         string
	ToolName      string
	ToolCallID    string
	ArgumentsJSON string
	Message       *planToolMessage
	RecordedAt    time.Time
}

type planToolMessage struct {
	Content string
	Extra   map[string]any
}

func evidenceForToolMessage(input toolMessageEvidenceInput) ([]model.PlanEvidence, error) {
	items := make([]model.PlanEvidence, 0, 4)
	if input.Message == nil {
		return items, nil
	}
	resultRef := toolResultRefFromMessage(input.Message.Extra)
	status := model.EvidenceStatusRecorded
	errText := strings.TrimSpace(toolErrorReason(input.Message))
	if errText != "" {
		status = model.EvidenceStatusFailed
	}
	base := model.PlanEvidence{
		ID:            fmt.Sprintf("%s-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:        input.Step.ID,
		Kind:          model.EvidenceKindTool,
		Status:        status,
		Summary:       tool.ExtractSemanticFact(input.ToolName, input.ArgumentsJSON, input.Message.Content),
		ToolResultRef: resultRef,
		ToolName:      input.ToolName,
		Command:       toolVerificationCommand(input.ToolName, input.ArgumentsJSON),
		Paths:         evidencePathsForTool(input.ToolName, input.ArgumentsJSON),
		Error:         errText,
		SourceRunID:   input.RunID,
		RecordedAt:    input.RecordedAt,
	}
	items = append(items, base)

	recorder := recorderFromMessageExtra(input.Message.Extra)
	for idx, item := range recorder.items {
		ev := model.PlanEvidence{
			ID:          fmt.Sprintf("%s-artifact-%d-%d", input.ToolName, input.RecordedAt.UnixNano(), idx),
			StepID:      input.Step.ID,
			Kind:        item.Kind,
			Status:      item.Status,
			Summary:     strings.TrimSpace(item.Summary),
			ToolName:    input.ToolName,
			Paths:       trimmedNonEmptyStrings(item.Paths),
			DiffRef:     strings.TrimSpace(item.DiffRef),
			Error:       strings.TrimSpace(item.Error),
			SourceRunID: input.RunID,
			RecordedAt:  input.RecordedAt,
		}
		items = append(items, ev)
	}

	if extra, err := commandOrTestEvidence(input, status); err != nil {
		return nil, err
	} else if extra != nil {
		items = append(items, *extra)
	}
	if extra := diffEvidenceFromTool(input); extra != nil {
		items = append(items, *extra)
	}
	if extra, err := delegatedSubagentEvidence(input); err != nil {
		return nil, err
	} else if extra != nil {
		items = append(items, *extra)
	}
	if extra, err := mutationCheckpointEvidence(input); err != nil {
		return nil, err
	} else if extra != nil {
		items = append(items, *extra)
	}
	if extra, err := rollbackEvidenceFromTool(input); err != nil {
		return nil, err
	} else if extra != nil {
		items = append(items, *extra)
	}
	if resultRef != "" {
		for i := range items {
			items[i].ToolResultRef = resultRef
		}
	}
	return items, nil
}

func delegatedSubagentEvidence(input toolMessageEvidenceInput) (*model.PlanEvidence, error) {
	if input.ToolName != "delegate_task" {
		return nil, nil
	}
	var payload orchestration.ChildAgentResult
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return nil, fmt.Errorf("parse delegate_task result: %w", err)
	}
	childRunID := strings.TrimSpace(payload.ChildRunID)
	if childRunID == "" {
		return nil, fmt.Errorf("parse delegate_task result: child_run_id is required")
	}
	acceptanceStatus := strings.TrimSpace(payload.Acceptance.Status)
	if acceptanceStatus == "" {
		return nil, fmt.Errorf("parse delegate_task result: acceptance.status is required")
	}
	status := model.EvidenceStatusPassed
	errorText := ""
	summary := fmt.Sprintf("child run %s passed acceptance", childRunID)
	if acceptanceStatus != "passed" {
		status = model.EvidenceStatusFailed
		errorText = strings.Join(trimmedNonEmptyStrings(payload.Acceptance.Reasons), "; ")
		summary = fmt.Sprintf("child run %s failed acceptance", childRunID)
		if errorText == "" {
			errorText = fmt.Sprintf("child run %s acceptance failed", childRunID)
		}
	}
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("%s-subagent-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        model.EvidenceKindSubagent,
		Status:      status,
		Summary:     summary,
		ToolName:    input.ToolName,
		ChildRunID:  childRunID,
		Error:       errorText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

func mutationCheckpointEvidence(input toolMessageEvidenceInput) (*model.PlanEvidence, error) {
	switch input.ToolName {
	case "create_file", "replace_span", "apply_unified_patch", "multi_edit":
	default:
		return nil, nil
	}
	if strings.TrimSpace(toolErrorReason(input.Message)) != "" {
		return nil, nil
	}
	var payload struct {
		CheckpointID    string   `json:"checkpoint_id"`
		CheckpointPaths []string `json:"checkpoint_paths"`
		Path            string   `json:"path"`
		Paths           []string `json:"paths"`
	}
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return nil, fmt.Errorf("parse %s checkpoint result: %w", input.ToolName, err)
	}
	checkpointID := strings.TrimSpace(payload.CheckpointID)
	if checkpointID == "" {
		return nil, fmt.Errorf("parse %s checkpoint result: checkpoint_id is required", input.ToolName)
	}
	paths := trimmedNonEmptyStrings(payload.CheckpointPaths)
	if len(paths) == 0 {
		paths = trimmedNonEmptyStrings(payload.Paths)
	}
	if len(paths) == 0 {
		paths = trimmedNonEmptyStrings([]string{payload.Path})
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("parse %s checkpoint result: checkpoint_paths are required", input.ToolName)
	}
	summary := fmt.Sprintf("workspace checkpoint %s recorded for %d path(s)", checkpointID, len(paths))
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("%s-checkpoint-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        model.EvidenceKindCheckpoint,
		Status:      model.EvidenceStatusPassed,
		Summary:     summary,
		ToolName:    input.ToolName,
		Paths:       paths,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

func rollbackEvidenceFromTool(input toolMessageEvidenceInput) (*model.PlanEvidence, error) {
	if input.ToolName != "rollback_workspace_checkpoint" {
		return nil, nil
	}
	var payload struct {
		CheckpointID  string   `json:"checkpoint_id"`
		RollbackID    string   `json:"rollback_id"`
		Status        string   `json:"status"`
		RestoredPaths []string `json:"restored_paths"`
		ConflictPaths []string `json:"conflict_paths"`
		Error         string   `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		if reason := strings.TrimSpace(toolErrorReason(input.Message)); reason != "" {
			return failedRollbackEvidence(input, reason), nil
		}
		return nil, fmt.Errorf("parse rollback_workspace_checkpoint result: %w", err)
	}
	errorText := strings.TrimSpace(payload.Error)
	var status model.EvidenceStatus
	var summary string
	rollbackID := strings.TrimSpace(payload.RollbackID)
	if rollbackID == "" {
		rollbackID = strings.TrimSpace(payload.CheckpointID)
	}
	if strings.TrimSpace(payload.Status) == "succeeded" {
		status = model.EvidenceStatusPassed
		summary = fmt.Sprintf("workspace rollback %s restored %d path(s)", rollbackID, len(trimmedNonEmptyStrings(payload.RestoredPaths)))
		if rollbackID == "" {
			return nil, fmt.Errorf("parse rollback_workspace_checkpoint result: rollback_id is required")
		}
	} else {
		status = model.EvidenceStatusFailed
		conflicts := trimmedNonEmptyStrings(payload.ConflictPaths)
		if errorText == "" && len(conflicts) > 0 {
			errorText = strings.Join(conflicts, ", ")
		}
		if errorText == "" {
			errorText = "workspace rollback failed"
		}
		if rollbackID == "" {
			rollbackID = "unknown"
		}
		summary = fmt.Sprintf("workspace rollback %s failed", rollbackID)
	}
	paths := trimmedNonEmptyStrings(payload.RestoredPaths)
	if len(paths) == 0 {
		paths = trimmedNonEmptyStrings(payload.ConflictPaths)
	}
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("rollback_workspace_checkpoint-%d", input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        model.EvidenceKindRollback,
		Status:      status,
		Summary:     summary,
		ToolName:    input.ToolName,
		Paths:       paths,
		Error:       errorText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

func failedRollbackEvidence(input toolMessageEvidenceInput, reason string) *model.PlanEvidence {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "workspace rollback failed"
	}
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("rollback_workspace_checkpoint-%d", input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        model.EvidenceKindRollback,
		Status:      model.EvidenceStatusFailed,
		Summary:     reason,
		ToolName:    input.ToolName,
		Error:       reason,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}
}

func commandOrTestEvidence(input toolMessageEvidenceInput, status model.EvidenceStatus) (*model.PlanEvidence, error) {
	if input.ToolName == "run_verification" {
		return runVerificationEvidence(input)
	}
	command := toolVerificationCommand(input.ToolName, input.ArgumentsJSON)
	if len(command) == 0 {
		return nil, nil
	}
	kind := model.EvidenceKindCommand
	if intentKinds(input.Step.VerificationIntent, "test") {
		kind = model.EvidenceKindTest
	}
	commandStatus := model.EvidenceStatusPassed
	errText := strings.TrimSpace(toolErrorReason(input.Message))
	if errText != "" {
		commandStatus = model.EvidenceStatusFailed
	}
	paths := append([]string(nil), intentPathsForKinds(input.Step.VerificationIntent, "test", "build", "lint")...)
	if len(paths) == 0 {
		paths = commandPathsFromArgs(input.ToolName, input.ArgumentsJSON)
	}
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("%s-command-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        kind,
		Status:      commandStatus,
		Summary:     tool.ExtractSemanticFact(input.ToolName, input.ArgumentsJSON, input.Message.Content),
		ToolName:    input.ToolName,
		Command:     command,
		Paths:       paths,
		Error:       errText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

func diffEvidenceFromTool(input toolMessageEvidenceInput) *model.PlanEvidence {
	if input.ToolName != "inspect_git_diff" && input.ToolName != "git_summary" {
		return nil
	}
	status := model.EvidenceStatusPassed
	if errText := strings.TrimSpace(toolErrorReason(input.Message)); errText != "" {
		status = model.EvidenceStatusFailed
	}
	paths := evidencePathsForTool(input.ToolName, input.ArgumentsJSON)
	diffRef := ""
	if input.ToolName == "git_summary" {
		var payload struct {
			DiffArtifactID string   `json:"diff_artifact_id"`
			ChangedPaths   []string `json:"changed_paths"`
		}
		if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err == nil {
			diffRef = strings.TrimSpace(payload.DiffArtifactID)
			if len(paths) == 0 {
				paths = trimmedNonEmptyStrings(payload.ChangedPaths)
			}
		}
		if diffRef == "" {
			return nil
		}
	}
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("%s-diff-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        model.EvidenceKindDiff,
		Status:      status,
		Summary:     tool.ExtractSemanticFact(input.ToolName, input.ArgumentsJSON, input.Message.Content),
		ToolName:    input.ToolName,
		Paths:       paths,
		DiffRef:     diffRef,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}
}

func runVerificationEvidence(input toolMessageEvidenceInput) (*model.PlanEvidence, error) {
	var payload struct {
		Kind             string   `json:"kind"`
		Status           string   `json:"status"`
		Summary          string   `json:"summary"`
		Command          []string `json:"command"`
		Paths            []string `json:"paths"`
		StdoutArtifactID string   `json:"stdout_artifact_id"`
		StderrArtifactID string   `json:"stderr_artifact_id"`
	}
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return nil, fmt.Errorf("parse run_verification result: %w", err)
	}
	command := trimmedNonEmptyStrings(payload.Command)
	if len(command) == 0 {
		command = toolVerificationCommand(input.ToolName, input.ArgumentsJSON)
	}
	if len(command) == 0 {
		return nil, fmt.Errorf("parse run_verification result: command is required")
	}
	kind := model.EvidenceKindCommand
	if strings.TrimSpace(payload.Kind) == "test" {
		kind = model.EvidenceKindTest
	}
	evidenceStatus := model.EvidenceStatusFailed
	errorText := ""
	if strings.TrimSpace(payload.Status) == "passed" {
		evidenceStatus = model.EvidenceStatusPassed
	} else {
		errorText = strings.TrimSpace(payload.Summary)
		if errorText == "" {
			errorText = fmt.Sprintf("%s verification failed", strings.TrimSpace(payload.Kind))
		}
	}
	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		summary = tool.ExtractSemanticFact(input.ToolName, input.ArgumentsJSON, input.Message.Content)
	}
	paths := trimmedNonEmptyStrings(payload.Paths)
	if len(paths) == 0 {
		paths = evidencePathsForTool(input.ToolName, input.ArgumentsJSON)
	}
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("%s-command-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        kind,
		Status:      evidenceStatus,
		Summary:     summary,
		ToolName:    input.ToolName,
		Command:     command,
		Paths:       paths,
		Error:       errorText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
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
	failed := false
	if rawFailed, ok := msg.Extra["tool_error"]; ok {
		if value, valueOK := rawFailed.(bool); valueOK {
			failed = value
		}
	}
	if !failed {
		return ""
	}
	reason := ""
	if rawReason, ok := msg.Extra["tool_error_reason"]; ok {
		if value, valueOK := rawReason.(string); valueOK {
			reason = value
		}
	}
	if strings.TrimSpace(reason) != "" {
		return reason
	}
	return strings.TrimSpace(msg.Content)
}

func evidencePathsForTool(toolName string, argumentsJSON string) []string {
	var payload struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &payload); err != nil {
		return nil
	}
	switch toolName {
	case "create_file", "replace_span", "inspect_git_diff":
		return trimmedNonEmptyStrings([]string{payload.Path})
	case "apply_unified_patch", "git_summary", "run_verification":
		return trimmedNonEmptyStrings(payload.Paths)
	case "multi_edit":
		var multiEdit struct {
			Edits []struct {
				Path string `json:"path"`
			} `json:"edits"`
		}
		if err := json.Unmarshal([]byte(argumentsJSON), &multiEdit); err != nil {
			return nil
		}
		paths := make([]string, 0, len(multiEdit.Edits))
		for _, edit := range multiEdit.Edits {
			paths = append(paths, edit.Path)
		}
		return trimmedNonEmptyStrings(paths)
	default:
		return nil
	}
}

func commandPathsFromArgs(toolName string, argumentsJSON string) []string {
	if toolName != "run_command" && toolName != "run_verification" {
		return nil
	}
	return evidencePathsForTool(toolName, argumentsJSON)
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

var (
	ErrRiskyToolRequiresPlan   = errors.New("risky tool execution requires an active persisted plan")
	ErrPlanStepVerificationGap = errors.New("plan step requires recorded verification before completion")
)

func enforceRiskyToolPlan(ctx context.Context, planStore runtimeapi.PlanStore, spec tooling.ToolSpec) (string, string, error) {
	if spec.PlanPolicy != tooling.PlanPolicyRequireActivePlan {
		return "", "", nil
	}
	if planStore == nil {
		return "", "", errors.New("plan enforcement store is not available")
	}
	sessionID := strings.TrimSpace(runtimeapi.SessionIDFromContext(ctx))
	if sessionID == "" {
		return "", "", fmt.Errorf("%w: session_id not available for %s", ErrRiskyToolRequiresPlan, spec.Name)
	}
	plan, err := planStore.LoadPlan(ctx, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrPlanNotFound) {
			return "", "", fmt.Errorf("%w: active plan not available before %s", ErrRiskyToolRequiresPlan, spec.Name)
		}
		return "", "", fmt.Errorf("load active plan for %s: %w", spec.Name, err)
	}
	stepIndex, err := graph.FindSingleInProgressPlanStep(plan)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrRiskyToolRequiresPlan, err)
	}
	return sessionID, plan.Steps[stepIndex].ID, nil
}
