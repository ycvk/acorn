package plan

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/runtime/tool"
)

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
	base, resultRef, status := evidenceBase(input)
	items = append(items, base)
	items = append(items, evidenceArtifacts(input)...)
	extras, err := evidenceExtras(input, status)
	if err != nil {
		return nil, err
	}
	items = append(items, extras...)
	if resultRef != "" {
		for i := range items {
			items[i].ToolResultRef = resultRef
		}
	}
	return items, nil
}

func evidenceBase(input toolMessageEvidenceInput) (model.PlanEvidence, string, model.EvidenceStatus) {
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
	return base, resultRef, status
}

func evidenceArtifacts(input toolMessageEvidenceInput) []model.PlanEvidence {
	recorder := recorderFromMessageExtra(input.Message.Extra)
	out := make([]model.PlanEvidence, 0, len(recorder.items))
	for idx, item := range recorder.items {
		out = append(out, model.PlanEvidence{
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
		})
	}
	return out
}

func evidenceExtras(input toolMessageEvidenceInput, status model.EvidenceStatus) ([]model.PlanEvidence, error) {
	var out []model.PlanEvidence
	if extra, err := commandOrTestEvidence(input, status); err != nil {
		return nil, err
	} else if extra != nil {
		out = append(out, *extra)
	}
	if extra := diffEvidenceFromTool(input); extra != nil {
		out = append(out, *extra)
	}
	if extra, err := delegatedSubagentEvidence(input); err != nil {
		return nil, err
	} else if extra != nil {
		out = append(out, *extra)
	}
	if extra, err := mutationCheckpointEvidence(input); err != nil {
		return nil, err
	} else if extra != nil {
		out = append(out, *extra)
	}
	if extra, err := rollbackEvidenceFromTool(input); err != nil {
		return nil, err
	} else if extra != nil {
		out = append(out, *extra)
	}
	return out, nil
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
		return multiEditPaths(argumentsJSON)
	default:
		return nil
	}
}

func multiEditPaths(argumentsJSON string) []string {
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
}

func commandPathsFromArgs(toolName string, argumentsJSON string) []string {
	if toolName != "run_command" && toolName != "run_verification" {
		return nil
	}
	return evidencePathsForTool(toolName, argumentsJSON)
}
