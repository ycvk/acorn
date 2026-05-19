package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/memorymodule"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
)

func emitProviderDegradedIfNeeded(ctx context.Context, store eventAppender, req RunnerBuildRequest, statuses []mcpprovider.ProviderStatus) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	var healthy, failed bool
	var failedEntries []ProviderDegradedEntry
	for _, s := range statuses {
		if !s.Enabled {
			continue
		}
		if s.StartupStatus == "healthy" {
			healthy = true
		} else if s.StartupStatus == "failed" {
			failed = true
			failedEntries = append(failedEntries, ProviderDegradedEntry{
				Name:      s.Name,
				Transport: s.Transport,
				Error:     s.Error,
			})
		}
	}
	if !healthy || !failed {
		return nil
	}
	_, err := appendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindProviderDegraded,
		CreatedAt: time.Now().UTC(),
		Payload: &ProviderDegradedPayload{
			AffectedProviders: failedEntries,
		},
	})
	return err
}

func emitMemoryPreparedEvent(ctx context.Context, store eventAppender, req RunnerBuildRequest, workspaceScope string, result *memorymodule.PrepareResult) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	prepared := &StreamMemoryPrepared{
		Query:          strings.TrimSpace(req.Input),
		WorkspaceScope: strings.TrimSpace(workspaceScope),
	}
	if result != nil {
		prepared.NudgeCount = len(result.Nudges)
		prepared.EntryCount = len(result.Entries)
		prepared.Nudges = make([]StreamMemoryPreparedNudge, 0, len(result.Nudges))
		for _, nudge := range result.Nudges {
			prepared.Nudges = append(prepared.Nudges, StreamMemoryPreparedNudge{
				Ref:    nudge.Ref,
				Kind:   nudge.Kind,
				Title:  nudge.Title,
				Status: nudge.Status,
				Reason: nudge.Reason,
			})
		}
		prepared.Entries = make([]StreamMemoryPreparedEntry, 0, len(result.Entries))
		for _, entry := range result.Entries {
			prepared.Entries = append(prepared.Entries, StreamMemoryPreparedEntry{
				Ref:   entry.Ref,
				Kind:  entry.Kind,
				Title: entry.Title,
			})
		}
	}
	_, err := appendStreamItem(ctx, store, req.Sink, StreamItem{
		RunID:     req.RunID,
		Kind:      StreamKindMemoryPrepared,
		CreatedAt: time.Now().UTC(),
		Payload:   &MemoryPreparedPayload{MemoryPrepared: prepared},
	})
	return err
}

func emitProcedureActivationEvents(ctx context.Context, store eventAppender, sink StreamSink, runID string, activations []memorymodule.ProcedureActivation) error {
	if store == nil || strings.TrimSpace(runID) == "" || len(activations) == 0 {
		return nil
	}
	for _, activation := range activations {
		_, err := appendStreamItem(ctx, store, sink, StreamItem{
			RunID:     runID,
			Kind:      StreamKindProcedureActivation,
			CreatedAt: time.Now().UTC(),
			Payload: &ProcedureActivationPayload{
				ProcedureActivation: streamProcedureActivationFromDomain(runID, activation),
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func filterProcedureActivationsByPhase(items []memorymodule.ProcedureActivation, phase memorymodule.ProcedureActivationPhase) []memorymodule.ProcedureActivation {
	if len(items) == 0 {
		return nil
	}
	out := make([]memorymodule.ProcedureActivation, 0, len(items))
	for _, item := range items {
		if item.Phase == phase {
			out = append(out, item)
		}
	}
	return out
}

func streamProcedureActivationFromDomain(runID string, item memorymodule.ProcedureActivation) *StreamProcedureActivation {
	effectiveRunID := strings.TrimSpace(item.RunID)
	if effectiveRunID == "" {
		effectiveRunID = strings.TrimSpace(runID)
	}
	return &StreamProcedureActivation{
		RunID:        effectiveRunID,
		SessionID:    strings.TrimSpace(item.SessionID),
		ProcedureRef: strings.TrimSpace(item.ProcedureRef),
		Title:        strings.TrimSpace(item.Title),
		Kind:         strings.TrimSpace(item.Kind),
		Phase:        string(item.Phase),
		Reason:       strings.TrimSpace(item.Reason),
		Score:        item.Score,
		Status:       string(item.Status),
		Origin:       string(item.Origin),
		SourceRefs:   append([]string(nil), item.SourceRefs...),
		EvidenceRefs: append([]string(nil), item.EvidenceRefs...),
	}
}

func buildStableInstruction(base string, instructionSuffix string) string {
	parts := []string{
		strings.TrimSpace(base),
		strings.TrimSpace(instructionSuffix),
	}
	out := make([]string, 0, len(parts))
	for _, item := range parts {
		if strings.TrimSpace(item) != "" {
			out = append(out, strings.TrimSpace(item))
		}
	}
	return strings.Join(out, "\n\n")
}
