package tools

import (
	"context"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/memory"
)

type MemoryService interface {
	Recall(context.Context, core.MemoryQuery) (core.MemoryRecall, error)
	Keep(context.Context, string, memory.WriteInput) (core.MemoryRecord, error)
	Think(context.Context, string, string, memory.WriteInput) (core.MemoryRecord, error)
	Correct(context.Context, string, memory.CorrectInput) (core.MemoryRecord, error)
	Forget(context.Context, string, core.MemoryForget) (core.MemoryExclusion, error)
	Read(context.Context, memory.ReadInput) (memory.ReadPage, error)
	Concern(context.Context, string, memory.ConcernInput) ([]core.MemoryConcern, error)
}

type PresenceToolDeps struct {
	Store         core.CommitmentStore
	Memory        MemoryService
	Context       core.ToolCallContextBridge
	Clock         func() time.Time
	Location      *time.Location
	ForgetBarrier func(context.Context, core.MemoryExclusion, string) error
}

func buildMemoryWriteTool(name, description string, deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool(name, description, func(ctx context.Context, in memory.WriteInput, _ ToolProgressEmitter) (core.MemoryRecord, error) {
		runID := deps.Context.CurrentRunID(ctx)
		if name == "keep" {
			return deps.Memory.Keep(ctx, runID, in)
		}
		return deps.Memory.Think(ctx, runID, deps.Context.CurrentSessionID(ctx), in)
	})
}

func buildRecallTool(deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("recall", "Find sourced memories by meaning, words, related events and time. mode=current/history; depth=standard/deep. Results report unprocessed sources; use memory_read to inspect evidence.", func(ctx context.Context, in core.MemoryQuery, _ ToolProgressEmitter) (core.MemoryRecall, error) {
		return deps.Memory.Recall(ctx, in)
	})
}

func buildMemoryReadTool(deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("memory_read", "Expand a memory's evidence, revisions and related records. Continue through source text with next_cursor.", func(ctx context.Context, in memory.ReadInput, _ ToolProgressEmitter) (memory.ReadPage, error) {
		return deps.Memory.Read(ctx, in)
	})
}

func buildMemoryCorrectTool(deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("memory_correct", "Correct a fact using the current owner's explicit correction, exact quote and the target revision. Previous validity remains available to historical recall.", func(ctx context.Context, in memory.CorrectInput, _ ToolProgressEmitter) (core.MemoryRecord, error) {
		return deps.Memory.Correct(ctx, deps.Context.CurrentRunID(ctx), in)
	})
}

func buildMemoryForgetTool(deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("memory_forget", "Exclude identified memories from future agent use. Requires the current owner's request source. Fact IDs exclude their supporting fragments; inference IDs withdraw the inference while preserving its underlying facts. source_ids exclude whole sources. Resolve ambiguous scope with the owner first.", func(ctx context.Context, in core.MemoryForget, _ ToolProgressEmitter) (core.MemoryForgetResult, error) {
		runID := deps.Context.CurrentRunID(ctx)
		exclusion, err := deps.Memory.Forget(ctx, runID, in)
		if err != nil {
			return core.MemoryForgetResult{}, err
		}
		if err := deps.ForgetBarrier(ctx, exclusion, runID); err != nil {
			return core.MemoryForgetResult{}, err
		}
		return core.MemoryForgetResult{Version: exclusion.Epoch, RecordIDs: exclusion.RecordIDs, SourceIDs: exclusion.SourceIDs, Scope: "Excluded from future agent memory and context. Original chat and knowledge files remain in their owning stores."}, nil
	})
}

func buildConcernTool(deps PresenceToolDeps) (einotool.BaseTool, error) {
	return inferProgressTool("concern", "Track a continuing goal or open issue. List before creating a related concern; update with its revision, reason, source and complete title/record links. Resolve when supported by owner confirmation or observed results.", func(ctx context.Context, in memory.ConcernInput, _ ToolProgressEmitter) ([]core.MemoryConcern, error) {
		return deps.Memory.Concern(ctx, deps.Context.CurrentRunID(ctx), in)
	})
}
