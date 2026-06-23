package tooldispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ycvk/acorn/internal/workspace"
)

// SideEffectRef records a durable side effect produced by a tool execution
// (mutation checkpoint, artifact write, operator action, etc.).
type SideEffectRef struct {
	Kind string
	Ref  string
	Path string
}

const (
	SideEffectKindOperatorAction = "operator_action"
	SideEffectKindArtifact       = "artifact"
)

func buildToolResultRef(runID, callID string) string {
	return "tool_result:" + strings.TrimSpace(runID) + ":" + strings.TrimSpace(callID)
}

func ToolSideEffectsFromResult(toolName string, result string) ([]SideEffectRef, error) {
	switch strings.TrimSpace(toolName) {
	case "create_file", "replace_span", "apply_unified_patch", "multi_edit":
		return mutationCheckpointSideEffects(toolName, result)
	case "rollback_workspace_checkpoint":
		return rollbackSideEffects(result)
	case "artifact_write":
		return artifactWriteSideEffects(result)
	case "run_verification":
		return runVerificationSideEffects(result)
	case "git_summary":
		return gitSummarySideEffects(result)
	case "ask_operator":
		return operatorQuestionSideEffects(result)
	default:
		return nil, nil
	}
}

func operatorQuestionSideEffects(result string) ([]SideEffectRef, error) {
	var payload struct {
		ActionID string `json:"action_id"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse ask_operator result: %w", err)
	}
	actionID := strings.TrimSpace(payload.ActionID)
	if actionID == "" {
		return nil, errors.New("ask_operator result missing action_id")
	}
	return []SideEffectRef{{
		Kind: SideEffectKindOperatorAction,
		Ref:  actionID,
	}}, nil
}

func artifactWriteSideEffects(result string) ([]SideEffectRef, error) {
	var payload struct {
		ArtifactID string `json:"artifact_id"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse artifact_write result: %w", err)
	}
	artifactID := strings.TrimSpace(payload.ArtifactID)
	if artifactID == "" {
		return nil, errors.New("artifact_write result missing artifact_id")
	}
	return []SideEffectRef{{
		Kind: SideEffectKindArtifact,
		Ref:  artifactID,
	}}, nil
}

func runVerificationSideEffects(result string) ([]SideEffectRef, error) {
	var payload struct {
		StdoutArtifactID string `json:"stdout_artifact_id"`
		StderrArtifactID string `json:"stderr_artifact_id"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse run_verification result: %w", err)
	}
	ids := normalizedSideEffectPaths([]string{payload.StdoutArtifactID, payload.StderrArtifactID})
	if len(ids) != 2 {
		return nil, errors.New("run_verification result missing stdout_artifact_id or stderr_artifact_id")
	}
	return []SideEffectRef{
		{Kind: SideEffectKindArtifact, Ref: ids[0]},
		{Kind: SideEffectKindArtifact, Ref: ids[1]},
	}, nil
}

func gitSummarySideEffects(result string) ([]SideEffectRef, error) {
	var payload struct {
		DiffArtifactID string `json:"diff_artifact_id"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse git_summary result: %w", err)
	}
	artifactID := strings.TrimSpace(payload.DiffArtifactID)
	if artifactID == "" {
		return nil, nil
	}
	return []SideEffectRef{{
		Kind: SideEffectKindArtifact,
		Ref:  artifactID,
	}}, nil
}

func mutationCheckpointSideEffects(toolName string, result string) ([]SideEffectRef, error) {
	var payload struct {
		CheckpointID    string   `json:"checkpoint_id"`
		CheckpointPaths []string `json:"checkpoint_paths"`
		Path            string   `json:"path"`
		Paths           []string `json:"paths"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse %s result: %w", toolName, err)
	}
	checkpointID := strings.TrimSpace(payload.CheckpointID)
	if checkpointID == "" {
		return nil, fmt.Errorf("%s result missing checkpoint_id", toolName)
	}
	paths := normalizedSideEffectPaths(payload.CheckpointPaths)
	if len(paths) == 0 {
		paths = normalizedSideEffectPaths(payload.Paths)
	}
	if len(paths) == 0 {
		paths = normalizedSideEffectPaths([]string{payload.Path})
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s result missing checkpoint_paths", toolName)
	}
	effects := make([]SideEffectRef, 0, len(paths))
	for _, path := range paths {
		effects = append(effects, SideEffectRef{
			Kind: workspace.MutationCheckpointEffect,
			Ref:  checkpointID,
			Path: path,
		})
	}
	return effects, nil
}

func rollbackSideEffects(result string) ([]SideEffectRef, error) {
	var payload struct {
		CheckpointID  string   `json:"checkpoint_id"`
		RollbackID    string   `json:"rollback_id"`
		Status        string   `json:"status"`
		RestoredPaths []string `json:"restored_paths"`
		ConflictPaths []string `json:"conflict_paths"`
		Error         string   `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return nil, fmt.Errorf("parse rollback result: %w", err)
	}
	if strings.TrimSpace(payload.Status) != "succeeded" {
		return nil, nil
	}
	rollbackID := strings.TrimSpace(payload.RollbackID)
	if rollbackID == "" {
		return nil, errors.New("rollback result missing rollback_id")
	}
	paths := normalizedSideEffectPaths(payload.RestoredPaths)
	if len(paths) == 0 {
		return nil, errors.New("rollback result missing restored_paths")
	}
	effects := make([]SideEffectRef, 0, len(paths))
	for _, path := range paths {
		effects = append(effects, SideEffectRef{
			Kind: workspace.MutationRollbackEffect,
			Ref:  rollbackID,
			Path: path,
		})
	}
	return effects, nil
}

func normalizedSideEffectPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		if trimmed := strings.TrimSpace(path); trimmed != "" {
			out = append(out, filepath.ToSlash(trimmed))
		}
	}
	return out
}
