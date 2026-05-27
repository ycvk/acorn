package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ycvk/acorn/internal/events"
	"github.com/ycvk/acorn/internal/model"
	"github.com/ycvk/acorn/internal/providers"
	"github.com/ycvk/acorn/internal/runtime"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/stream"
	"github.com/ycvk/acorn/internal/workspace"
)

func flattenPlanEvidence(plan *model.Plan) []model.PlanEvidence {
	if plan == nil {
		return nil
	}
	evidence := make([]model.PlanEvidence, 0)
	for _, step := range plan.Steps {
		evidence = append(evidence, step.Evidence...)
	}
	sort.SliceStable(evidence, func(i, j int) bool {
		return evidence[i].RecordedAt.After(evidence[j].RecordedAt)
	})
	if len(evidence) > 10 {
		evidence = evidence[:10]
	}
	return evidence
}

func buildContextEconomySummary(rawEvents []events.EventRecord, records []store.ToolResultRecord) ContextEconomySummary {
	summary := ContextEconomySummary{
		ToolResultCount: len(records),
		ToolResults:     make([]ContextToolResultSummary, 0, len(records)),
	}
	for _, record := range records {
		item := ContextToolResultSummary{
			ResultRef:     record.ResultRef,
			ToolName:      record.ToolName,
			Status:        string(record.Status),
			Preview:       record.Preview,
			TokenEstimate: record.TokenEstimate,
			FullTextBytes: len([]byte(record.FullText)),
			Elided:        strings.TrimSpace(record.Preview) != strings.TrimSpace(record.FullText),
			EvidenceRefs:  evidenceRefStrings(record.EvidenceRefs),
		}
		if item.Elided {
			summary.ElidedToolResultCount++
		}
		summary.ToolResultTokenEstimate += record.TokenEstimate
		summary.ToolResults = append(summary.ToolResults, item)
	}
	sort.SliceStable(summary.ToolResults, func(i, j int) bool {
		return summary.ToolResults[i].ResultRef < summary.ToolResults[j].ResultRef
	})

	trace := runtime.BuildTrace(nil, rawEvents)
	for _, item := range trace.Items {
		if pressure := item.GetContextPressure(); pressure != nil {
			summary.LatestPressure = &ContextPressureSummary{
				State:                 pressure.State,
				EstimatedInputTokens:  pressure.EstimatedInputTokens,
				EffectiveWindowTokens: pressure.EffectiveWindowTokens,
				PercentUsed:           pressure.PercentUsed,
			}
		}
		if compressed := item.GetContextCompressed(); compressed != nil {
			summary.LatestCompression = &ContextCompressionSummary{
				BoundaryID:   compressed.BoundaryID,
				TokensBefore: compressed.TokensBefore,
				TokensAfter:  compressed.TokensAfter,
				Summary:      compressed.SummarySnippet,
			}
		}
		if prepared := item.GetMemoryPrepared(); prepared != nil {
			for _, entry := range prepared.Entries {
				summary.MemoryRefs = appendUniqueStrings(summary.MemoryRefs, []string{entry.Ref})
			}
			for _, nudge := range prepared.Nudges {
				summary.MemoryRefs = appendUniqueStrings(summary.MemoryRefs, []string{nudge.Ref})
			}
		}
		if activation := item.GetProcedureActivation(); activation != nil {
			summary.ProcedureRefs = appendUniqueStrings(summary.ProcedureRefs, []string{activation.ProcedureRef})
		}
	}
	return summary
}

func buildArtifactSummaries(records []store.ArtifactRecord) []ArtifactSummary {
	if len(records) == 0 {
		return nil
	}
	items := make([]ArtifactSummary, 0, len(records))
	for _, record := range records {
		items = append(items, ArtifactSummary{
			ArtifactID:          record.ArtifactID,
			RunID:               record.RunID,
			SessionID:           record.SessionID,
			SourceToolResultRef: record.SourceToolResultRef,
			Kind:                string(record.Kind),
			Title:               record.Title,
			MIMEType:            record.MIMEType,
			SizeBytes:           record.SizeBytes,
			SHA256:              record.SHA256,
			CreatedAt:           record.CreatedAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ArtifactID < items[j].ArtifactID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func buildProviderUsageSummary(records []providers.UsageRecord) ProviderUsageSummary {
	summary := ProviderUsageSummary{
		CallCount: len(records),
		Records:   make([]ProviderUsageCallSummary, 0, len(records)),
	}
	for _, record := range records {
		summary.PromptTokens += record.PromptTokens
		summary.CompletionTokens += record.CompletionTokens
		summary.TotalTokens += record.TotalTokens
		summary.CachedTokens += record.CachedTokens
		summary.ReasoningTokens += record.ReasoningTokens
		summary.Records = append(summary.Records, ProviderUsageCallSummary{
			UsageID:          record.UsageID,
			CallSite:         record.CallSite,
			ProviderName:     record.ProviderName,
			ModelName:        record.ModelName,
			PromptTokens:     record.PromptTokens,
			CompletionTokens: record.CompletionTokens,
			TotalTokens:      record.TotalTokens,
			CachedTokens:     record.CachedTokens,
			ReasoningTokens:  record.ReasoningTokens,
			CreatedAt:        record.CreatedAt,
		})
	}
	sort.SliceStable(summary.Records, func(i, j int) bool {
		return summary.Records[i].CreatedAt.Before(summary.Records[j].CreatedAt)
	})
	return summary
}

func evidenceRefStrings(items []store.EvidenceRef) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Ref) != "" {
			out = append(out, strings.TrimSpace(item.Ref))
		}
	}
	return out
}

func buildSubagentRuns(raw []events.EventRecord) []SubagentRun {
	if len(raw) == 0 {
		return nil
	}
	byID := make(map[string]*SubagentRun)
	order := make([]string, 0)
	for _, event := range raw {
		item := runtime.BuildTrace(nil, []events.EventRecord{event}).Items
		if len(item) == 0 {
			continue
		}
		current := item[0]
		switch current.Kind {
		case stream.StreamKindSubagentStarted:
			payload, ok := current.Payload.(*stream.SubagentStartedPayload)
			if !ok || strings.TrimSpace(payload.SubRunID) == "" {
				continue
			}
			run := &SubagentRun{
				SubRunID:          payload.SubRunID,
				ParentRunID:       payload.ParentID,
				SessionID:         payload.SessionID,
				Depth:             payload.Depth,
				Task:              payload.Task,
				ChildRunMode:      payload.ChildRunMode,
				WorkspaceMode:     payload.WorkspaceMode,
				WorktreePath:      payload.WorktreePath,
				ContextMessages:   payload.ContextMessages,
				OrchestrationMode: payload.OrchestrationMode,
				ParentStepID:      payload.ParentStepID,
				State:             "started",
				UpdatedAt:         current.CreatedAt,
			}
			byID[payload.SubRunID] = run
			order = append(order, payload.SubRunID)
		case stream.StreamKindSubagentCompleted:
			payload, ok := current.Payload.(*stream.SubagentCompletedPayload)
			if !ok || strings.TrimSpace(payload.SubRunID) == "" {
				continue
			}
			run, ok := byID[payload.SubRunID]
			if !ok {
				run = &SubagentRun{SubRunID: payload.SubRunID}
				byID[payload.SubRunID] = run
				order = append(order, payload.SubRunID)
			}
			run.ParentRunID = payload.ParentID
			run.SessionID = payload.SessionID
			run.OrchestrationMode = payload.OrchestrationMode
			run.ParentStepID = payload.ParentStepID
			run.State = "completed"
			run.FinalStatus = payload.FinalStatus
			run.AcceptanceStatus = payload.AcceptanceStatus
			run.AcceptanceReasons = append([]string(nil), payload.AcceptanceReasons...)
			run.ChildRunMode = payload.ChildRunMode
			run.WorkspaceMode = payload.WorkspaceMode
			run.WorktreePath = payload.WorktreePath
			run.EvidenceRefs = append([]string(nil), payload.EvidenceRefs...)
			run.Summary = payload.Summary
			run.UpdatedAt = current.CreatedAt
		case stream.StreamKindSubagentFailed:
			payload, ok := current.Payload.(*stream.SubagentFailedPayload)
			if !ok || strings.TrimSpace(payload.SubRunID) == "" {
				continue
			}
			run, ok := byID[payload.SubRunID]
			if !ok {
				run = &SubagentRun{SubRunID: payload.SubRunID}
				byID[payload.SubRunID] = run
				order = append(order, payload.SubRunID)
			}
			run.ParentRunID = payload.ParentID
			run.SessionID = payload.SessionID
			run.OrchestrationMode = payload.OrchestrationMode
			run.ParentStepID = payload.ParentStepID
			run.State = "failed"
			run.AcceptanceStatus = payload.AcceptanceStatus
			run.AcceptanceReasons = append([]string(nil), payload.AcceptanceReasons...)
			run.ChildRunMode = payload.ChildRunMode
			run.WorkspaceMode = payload.WorkspaceMode
			run.WorktreePath = payload.WorktreePath
			run.Summary = payload.Error
			run.UpdatedAt = current.CreatedAt
		}
	}
	result := make([]SubagentRun, 0, len(order))
	for _, id := range order {
		if run, ok := byID[id]; ok {
			result = append(result, *run)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result
}

func buildMutationCheckpointSummaries(records []store.ToolResultRecord) []MutationCheckpointSummary {
	byID := make(map[string]*MutationCheckpointSummary)
	for _, record := range records {
		paths := sideEffectPaths(record.SideEffects, workspace.MutationCheckpointEffect)
		if len(paths) == 0 {
			continue
		}
		checkpointID := firstSideEffectRef(record.SideEffects, workspace.MutationCheckpointEffect)
		if checkpointID == "" {
			continue
		}
		summary := byID[checkpointID]
		if summary == nil {
			summary = &MutationCheckpointSummary{CheckpointID: checkpointID}
			byID[checkpointID] = summary
		}
		summary.ToolResultRef = record.ResultRef
		summary.ToolName = record.ToolName
		summary.Status = string(record.Status)
		summary.Summary = record.Preview
		summary.CreatedAt = record.CreatedAt
		summary.Paths = appendUniqueStrings(summary.Paths, paths)
		if diffStat := mutationCheckpointDiffStat(record.FullText); diffStat != "" {
			summary.VerifiedDiffStat = diffStat
		}
	}
	return sortedMutationCheckpointSummaries(byID)
}

func buildRollbackSummaries(records []store.ToolResultRecord) ([]RollbackSummary, error) {
	byID := make(map[string]*RollbackSummary)
	for _, record := range records {
		if strings.TrimSpace(record.ToolName) != "rollback_workspace_checkpoint" {
			continue
		}
		parsed, err := parseRollbackSummaryRecord(record)
		if err != nil {
			return nil, err
		}
		summary := byID[parsed.RollbackID]
		if summary == nil {
			summary = &RollbackSummary{RollbackID: parsed.RollbackID}
			byID[parsed.RollbackID] = summary
		}
		summary.CheckpointID = parsed.CheckpointID
		summary.ToolResultRef = record.ResultRef
		summary.ToolName = record.ToolName
		summary.Status = parsed.Status
		summary.RestoredPaths = appendUniqueStrings(summary.RestoredPaths, parsed.RestoredPaths)
		summary.ConflictPaths = appendUniqueStrings(summary.ConflictPaths, parsed.ConflictPaths)
		summary.Summary = record.Preview
		summary.Error = parsed.Error
		summary.CreatedAt = record.CreatedAt
	}
	return sortedRollbackSummaries(byID), nil
}

type rollbackSummaryPayload struct {
	CheckpointID  string   `json:"checkpoint_id"`
	RollbackID    string   `json:"rollback_id"`
	Status        string   `json:"status"`
	RestoredPaths []string `json:"restored_paths"`
	ConflictPaths []string `json:"conflict_paths"`
	Error         string   `json:"error"`
}

func parseRollbackSummaryRecord(record store.ToolResultRecord) (RollbackSummary, error) {
	var payload rollbackSummaryPayload
	if err := json.Unmarshal([]byte(record.FullText), &payload); err != nil {
		if record.Status == store.ToolResultStatusFailed {
			return failedRollbackSummaryRecord(record), nil
		}
		return RollbackSummary{}, fmt.Errorf("parse rollback_workspace_checkpoint result %s: %w", record.ResultRef, err)
	}
	rollbackID := strings.TrimSpace(payload.RollbackID)
	if rollbackID == "" {
		rollbackID = strings.TrimSpace(record.ResultRef)
	}
	status := strings.TrimSpace(payload.Status)
	if status == "" {
		status = string(record.Status)
	}
	return RollbackSummary{
		RollbackID:    rollbackID,
		CheckpointID:  strings.TrimSpace(payload.CheckpointID),
		ToolResultRef: record.ResultRef,
		ToolName:      record.ToolName,
		Status:        status,
		RestoredPaths: trimmedWorkspacePaths(payload.RestoredPaths),
		ConflictPaths: trimmedWorkspacePaths(payload.ConflictPaths),
		Summary:       record.Preview,
		Error:         strings.TrimSpace(payload.Error),
		CreatedAt:     record.CreatedAt,
	}, nil
}

func failedRollbackSummaryRecord(record store.ToolResultRecord) RollbackSummary {
	errText := strings.TrimSpace(record.ErrorReason)
	if errText == "" {
		errText = strings.TrimSpace(record.Preview)
	}
	if errText == "" {
		errText = strings.TrimSpace(record.FullText)
	}
	if errText == "" {
		errText = "workspace rollback failed"
	}
	return RollbackSummary{
		RollbackID:    strings.TrimSpace(record.ResultRef),
		ToolResultRef: record.ResultRef,
		ToolName:      record.ToolName,
		Status:        string(record.Status),
		Summary:       record.Preview,
		Error:         errText,
		CreatedAt:     record.CreatedAt,
	}
}

func mutationCheckpointDiffStat(fullText string) string {
	var payload struct {
		VerifiedDiffStat string `json:"verified_diff_stat"`
	}
	if err := json.Unmarshal([]byte(fullText), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.VerifiedDiffStat)
}

func sideEffectPaths(items []store.SideEffectRef, kind string) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Kind) != kind {
			continue
		}
		if path := strings.TrimSpace(item.Path); path != "" {
			paths = append(paths, path)
		}
	}
	return trimmedWorkspacePaths(paths)
}

func firstSideEffectRef(items []store.SideEffectRef, kind string) string {
	for _, item := range items {
		if strings.TrimSpace(item.Kind) == kind && strings.TrimSpace(item.Ref) != "" {
			return strings.TrimSpace(item.Ref)
		}
	}
	return ""
}

func trimmedWorkspacePaths(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, filepath.ToSlash(trimmed))
		}
	}
	return out
}

func appendUniqueStrings(base []string, values []string) []string {
	seen := make(map[string]struct{}, len(base))
	for _, item := range base {
		seen[strings.TrimSpace(item)] = struct{}{}
	}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		base = append(base, trimmed)
	}
	return base
}

func sortedMutationCheckpointSummaries(byID map[string]*MutationCheckpointSummary) []MutationCheckpointSummary {
	result := make([]MutationCheckpointSummary, 0, len(byID))
	for _, summary := range byID {
		if summary != nil {
			result = append(result, *summary)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CheckpointID < result[j].CheckpointID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func sortedRollbackSummaries(byID map[string]*RollbackSummary) []RollbackSummary {
	result := make([]RollbackSummary, 0, len(byID))
	for _, summary := range byID {
		if summary != nil {
			result = append(result, *summary)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].RollbackID < result[j].RollbackID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

func runtimeWorkbenchNextStepHint(workbench *RuntimeWorkbench) string {
	if workbench == nil {
		return ""
	}
	if workbench.Resumable {
		return "可以继续当前工作，从上次中断处恢复。"
	}
	switch workbench.State {
	case runtimeapi.SessionStateRunning:
		return "先查看最近一次轨迹，确认当前运行是否仍在进行中。"
	case runtimeapi.SessionStateFailed:
		return "先查看失败轨迹，再决定继续当前工作还是补发新消息。"
	case runtimeapi.SessionStateCompleted:
		return "可以基于当前上下文继续推进，或发送新消息开始下一步。"
	case runtimeapi.SessionStateInterrupted:
		return "当前运行已中断，先检查中断上下文和最近证据。"
	case runtimeapi.SessionStateNew:
		return "新建本地会话开始新的工作。"
	default:
		if strings.TrimSpace(workbench.LatestRunID) != "" {
			return "先查看最近一次运行和轨迹，再继续当前工作。"
		}
		return "发送第一条消息开始新的工作。"
	}
}
