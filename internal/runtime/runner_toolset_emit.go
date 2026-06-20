package runtime

import (
	"context"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/memorymodule"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/stream"
)

func emitMemoryPreparedEvent(ctx context.Context, store runtimeapi.EventAppender, req RunnerBuildRequest, workspaceScope string, result *memorymodule.PrepareResult) error {
	if store == nil || strings.TrimSpace(req.RunID) == "" {
		return nil
	}
	prepared := &stream.StreamMemoryPrepared{
		Query:          strings.TrimSpace(req.Input),
		WorkspaceScope: strings.TrimSpace(workspaceScope),
	}
	if result != nil {
		prepared.NudgeCount = len(result.Nudges)
		prepared.EntryCount = len(result.Entries)
		prepared.Nudges = streamMemoryNudges(result.Nudges)
		prepared.Entries = streamMemoryEntries(result.Entries)
	}
	_, err := stream.AppendStreamItem(ctx, store, req.Sink, stream.StreamItem{
		RunID:     req.RunID,
		Kind:      stream.StreamKindMemoryPrepared,
		CreatedAt: time.Now().UTC(),
		Payload:   map[string]any{"memory_prepared": prepared},
	})
	return err
}

func streamMemoryNudges(nudges []memorymodule.Nudge) []stream.StreamMemoryPreparedNudge {
	out := make([]stream.StreamMemoryPreparedNudge, 0, len(nudges))
	for _, n := range nudges {
		out = append(out, stream.StreamMemoryPreparedNudge{
			Ref: n.Ref, Kind: n.Kind, Title: n.Title, Status: n.Status, Reason: n.Reason,
		})
	}
	return out
}

func streamMemoryEntries(entries []memorymodule.Entry) []stream.StreamMemoryPreparedEntry {
	out := make([]stream.StreamMemoryPreparedEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, stream.StreamMemoryPreparedEntry{
			Ref: e.Ref, Kind: e.Kind, Title: e.Title,
		})
	}
	return out
}

func emitProcedureActivationEvents(ctx context.Context, store runtimeapi.EventAppender, sink stream.StreamSink, runID string, activations []memorymodule.ProcedureActivation) error {
	if store == nil || strings.TrimSpace(runID) == "" || len(activations) == 0 {
		return nil
	}
	for _, activation := range activations {
		_, err := stream.AppendStreamItem(ctx, store, sink, stream.StreamItem{
			RunID:     runID,
			Kind:      stream.StreamKindProcedureActivation,
			CreatedAt: time.Now().UTC(),
			Payload:   map[string]any{"procedure_activation": streamProcedureActivationFromDomain(runID, activation)},
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

func streamProcedureActivationFromDomain(runID string, item memorymodule.ProcedureActivation) *stream.StreamProcedureActivation {
	effectiveRunID := strings.TrimSpace(item.RunID)
	if effectiveRunID == "" {
		effectiveRunID = strings.TrimSpace(runID)
	}
	return &stream.StreamProcedureActivation{
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
