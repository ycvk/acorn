package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/runtime/tool"
)

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
	commandStatus, errText := commandEvidenceStatus(input, status)
	paths := commandEvidencePaths(input)
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

func commandEvidenceStatus(input toolMessageEvidenceInput, status model.EvidenceStatus) (model.EvidenceStatus, string) {
	commandStatus := model.EvidenceStatusPassed
	errText := strings.TrimSpace(toolErrorReason(input.Message))
	if errText != "" {
		commandStatus = model.EvidenceStatusFailed
	}
	return commandStatus, errText
}

func commandEvidencePaths(input toolMessageEvidenceInput) []string {
	paths := append([]string(nil), intentPathsForKinds(input.Step.VerificationIntent, "test", "build", "lint")...)
	if len(paths) == 0 {
		paths = commandPathsFromArgs(input.ToolName, input.ArgumentsJSON)
	}
	return paths
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
		diffRef, paths = gitSummaryDiff(input, paths)
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

func gitSummaryDiff(input toolMessageEvidenceInput, paths []string) (string, []string) {
	if input.ToolName != "git_summary" {
		return "", paths
	}
	var payload struct {
		DiffArtifactID string   `json:"diff_artifact_id"`
		ChangedPaths   []string `json:"changed_paths"`
	}
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return "", paths
	}
	diffRef := strings.TrimSpace(payload.DiffArtifactID)
	if len(paths) == 0 {
		paths = trimmedNonEmptyStrings(payload.ChangedPaths)
	}
	return diffRef, paths
}

func runVerificationEvidence(input toolMessageEvidenceInput) (*model.PlanEvidence, error) {
	payload, err := parseRunVerificationPayload(input)
	if err != nil {
		return nil, err
	}
	command := runVerificationCommand(input, payload)
	if len(command) == 0 {
		return nil, fmt.Errorf("parse run_verification result: command is required")
	}
	kind := model.EvidenceKindCommand
	if strings.TrimSpace(payload.Kind) == "test" {
		kind = model.EvidenceKindTest
	}
	status, errorText := runVerificationStatus(payload)
	summary, paths := runVerificationSummaryAndPaths(input, payload)
	return &model.PlanEvidence{
		ID:          fmt.Sprintf("%s-command-%d", input.ToolName, input.RecordedAt.UnixNano()),
		StepID:      input.Step.ID,
		Kind:        kind,
		Status:      status,
		Summary:     summary,
		ToolName:    input.ToolName,
		Command:     command,
		Paths:       paths,
		Error:       errorText,
		SourceRunID: input.RunID,
		RecordedAt:  input.RecordedAt,
	}, nil
}

type runVerificationPayload struct {
	Kind             string   `json:"kind"`
	Status           string   `json:"status"`
	Summary          string   `json:"summary"`
	Command          []string `json:"command"`
	Paths            []string `json:"paths"`
	StdoutArtifactID string   `json:"stdout_artifact_id"`
	StderrArtifactID string   `json:"stderr_artifact_id"`
}

func parseRunVerificationPayload(input toolMessageEvidenceInput) (runVerificationPayload, error) {
	var payload runVerificationPayload
	if err := json.Unmarshal(bytes.TrimSpace([]byte(input.Message.Content)), &payload); err != nil {
		return runVerificationPayload{}, fmt.Errorf("parse run_verification result: %w", err)
	}
	return payload, nil
}

func runVerificationCommand(input toolMessageEvidenceInput, payload runVerificationPayload) []string {
	command := trimmedNonEmptyStrings(payload.Command)
	if len(command) == 0 {
		command = toolVerificationCommand(input.ToolName, input.ArgumentsJSON)
	}
	return command
}

func runVerificationStatus(payload runVerificationPayload) (model.EvidenceStatus, string) {
	if strings.TrimSpace(payload.Status) == "passed" {
		return model.EvidenceStatusPassed, ""
	}
	errorText := strings.TrimSpace(payload.Summary)
	if errorText == "" {
		errorText = fmt.Sprintf("%s verification failed", strings.TrimSpace(payload.Kind))
	}
	return model.EvidenceStatusFailed, errorText
}

func runVerificationSummaryAndPaths(input toolMessageEvidenceInput, payload runVerificationPayload) (string, []string) {
	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		summary = tool.ExtractSemanticFact(input.ToolName, input.ArgumentsJSON, input.Message.Content)
	}
	paths := trimmedNonEmptyStrings(payload.Paths)
	if len(paths) == 0 {
		paths = evidencePathsForTool(input.ToolName, input.ArgumentsJSON)
	}
	return summary, paths
}
