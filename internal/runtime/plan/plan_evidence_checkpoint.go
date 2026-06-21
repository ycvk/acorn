package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/model"
)

func mutationCheckpointEvidence(input toolMessageEvidenceInput) (*model.PlanEvidence, error) {
	if !isMutationCheckpointTool(input.ToolName) {
		return nil, nil
	}
	if strings.TrimSpace(toolErrorReason(input.Message)) != "" {
		return nil, nil
	}
	payload, err := parseCheckpointPayload(input)
	if err != nil {
		return nil, err
	}
	checkpointID := strings.TrimSpace(payload.CheckpointID)
	if checkpointID == "" {
		return nil, fmt.Errorf("parse %s checkpoint result: checkpoint_id is required", input.ToolName)
	}
	paths := checkpointPaths(payload)
	if len(paths) == 0 {
		return nil, fmt.Errorf("parse %s checkpoint result: checkpoint_paths are required", input.ToolName)
	}
	return checkpointEvidenceRecord(input, checkpointID, paths), nil
}

func isMutationCheckpointTool(toolName string) bool {
	switch toolName {
	case "create_file", "replace_span", "apply_unified_patch", "multi_edit":
		return true
	default:
		return false
	}
}

func parseCheckpointPayload(input toolMessageEvidenceInput) (checkpointPayload, error) {
	var payload checkpointPayload
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return checkpointPayload{}, fmt.Errorf("parse %s checkpoint result: %w", input.ToolName, err)
	}
	return payload, nil
}

type checkpointPayload struct {
	CheckpointID    string   `json:"checkpoint_id"`
	CheckpointPaths []string `json:"checkpoint_paths"`
	Path            string   `json:"path"`
	Paths           []string `json:"paths"`
}

func checkpointPaths(payload checkpointPayload) []string {
	paths := trimmedNonEmptyStrings(payload.CheckpointPaths)
	if len(paths) == 0 {
		paths = trimmedNonEmptyStrings(payload.Paths)
	}
	if len(paths) == 0 {
		paths = trimmedNonEmptyStrings([]string{payload.Path})
	}
	return paths
}

func checkpointEvidenceRecord(input toolMessageEvidenceInput, checkpointID string, paths []string) *model.PlanEvidence {
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
	}
}

func rollbackEvidenceFromTool(input toolMessageEvidenceInput) (*model.PlanEvidence, error) {
	if input.ToolName != "rollback_workspace_checkpoint" {
		return nil, nil
	}
	payload, err := parseRollbackPayload(input)
	if err != nil {
		if reason := strings.TrimSpace(toolErrorReason(input.Message)); reason != "" {
			return failedRollbackEvidence(input, reason), nil
		}
		return nil, err
	}
	rollbackID := rollbackIdentifier(payload)
	if strings.TrimSpace(payload.Status) == "succeeded" {
		if rollbackID == "" {
			return nil, fmt.Errorf("parse rollback_workspace_checkpoint result: rollback_id is required")
		}
		return rollbackSuccessRecord(input, rollbackID, payload), nil
	}
	return rollbackFailureRecord(input, rollbackID, payload), nil
}

func parseRollbackPayload(input toolMessageEvidenceInput) (rollbackPayload, error) {
	var payload rollbackPayload
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return rollbackPayload{}, fmt.Errorf("parse rollback_workspace_checkpoint result: %w", err)
	}
	return payload, nil
}

type rollbackPayload struct {
	CheckpointID  string   `json:"checkpoint_id"`
	RollbackID    string   `json:"rollback_id"`
	Status        string   `json:"status"`
	RestoredPaths []string `json:"restored_paths"`
	ConflictPaths []string `json:"conflict_paths"`
	Error         string   `json:"error"`
}

func rollbackIdentifier(payload rollbackPayload) string {
	rollbackID := strings.TrimSpace(payload.RollbackID)
	if rollbackID == "" {
		rollbackID = strings.TrimSpace(payload.CheckpointID)
	}
	return rollbackID
}

func rollbackSuccessRecord(input toolMessageEvidenceInput, rollbackID string, payload rollbackPayload) *model.PlanEvidence {
	restored := trimmedNonEmptyStrings(payload.RestoredPaths)
	summary := fmt.Sprintf("workspace rollback %s restored %d path(s)", rollbackID, len(restored))
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("rollback_workspace_checkpoint-%d", input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        model.EvidenceKindRollback,
		Status:      model.EvidenceStatusPassed,
		Summary:     summary,
		ToolName:    input.ToolName,
		Paths:       restored,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}
}

func rollbackFailureRecord(input toolMessageEvidenceInput, rollbackID string, payload rollbackPayload) *model.PlanEvidence {
	if rollbackID == "" {
		rollbackID = "unknown"
	}
	errorText := strings.TrimSpace(payload.Error)
	conflicts := trimmedNonEmptyStrings(payload.ConflictPaths)
	if errorText == "" && len(conflicts) > 0 {
		errorText = strings.Join(conflicts, ", ")
	}
	if errorText == "" {
		errorText = "workspace rollback failed"
	}
	paths := trimmedNonEmptyStrings(payload.RestoredPaths)
	if len(paths) == 0 {
		paths = conflicts
	}
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("rollback_workspace_checkpoint-%d", input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        model.EvidenceKindRollback,
		Status:      model.EvidenceStatusFailed,
		Summary:     fmt.Sprintf("workspace rollback %s failed", rollbackID),
		ToolName:    input.ToolName,
		Paths:       paths,
		Error:       errorText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}
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
