package tool

import (
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/ycvk/acorn/internal/contextplane"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/store"
	"github.com/ycvk/acorn/internal/store/storetest"
)

// NewTestToolLifecycleContext creates a tool lifecycle context suitable for tests.
// It registers all tools in the node as loaded and attaches the lifecycle to ctx.
func NewTestToolLifecycleContext(ctx context.Context, node *SafeParallelToolsNode) context.Context {
	sessionID := runtimeapi.SessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "sess_test"
		ctx = runtimeapi.WithSessionID(ctx, sessionID)
	}
	runID := runtimeapi.GetRunID(ctx)
	if strings.TrimSpace(runID) == "" {
		runID = "run_test"
		ctx = runtimeapi.WithRunID(ctx, runID)
	}
	state := &contextplane.ToolLifecycleState{
		RunID:         runID,
		SessionID:     sessionID,
		LoadedTools:   make(map[string]contextplane.LoadedToolRecord, len(node.tools)),
		DeferredTools: make(map[string]contextplane.DeferredToolRecord),
		MaxAgeTurns:   2,
		MaxResultRefs: 32,
	}
	infos := make([]*schema.ToolInfo, 0, len(node.tools))
	for name, entry := range node.tools {
		state.LoadedTools[name] = contextplane.LoadedToolRecord{Name: name, LoadSource: "test"}
		info, err := entry.Tool.Info(context.Background())
		if err != nil {
			continue
		}
		if info != nil {
			infos = append(infos, info)
		}
	}
	return contextplane.WithToolLifecycleContext(ctx, storetest.NewMemoryToolResultLedger(), state, nil, infos)
}

// MustInferTool infers a tool from a function and fails the test on error.
func MustInferTool[T any, R any](t *testing.T, name string, fn func(context.Context, T) (R, error)) einotool.BaseTool {
	t.Helper()
	tool, err := toolutils.InferTool(name, name, fn)
	if err != nil {
		t.Fatalf("infer tool: %v", err)
	}
	return tool
}

// SafeParallelLifecycleContextFromWithLedger creates a tool lifecycle context for tests
// with an explicit tool-result ledger. If ledger is nil, it falls back to NewTestToolLifecycleContext.
func SafeParallelLifecycleContextFromWithLedger(t *testing.T, ctx context.Context, node *SafeParallelToolsNode, ledger store.ToolResultLedger) context.Context {
	t.Helper()
	if ledger == nil {
		return NewTestToolLifecycleContext(ctx, node)
	}
	sessionID := runtimeapi.SessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "sess_test"
		ctx = runtimeapi.WithSessionID(ctx, sessionID)
	}
	runID := runtimeapi.GetRunID(ctx)
	if strings.TrimSpace(runID) == "" {
		runID = "run_test"
		ctx = runtimeapi.WithRunID(ctx, runID)
	}
	state := &contextplane.ToolLifecycleState{
		RunID:         runID,
		SessionID:     sessionID,
		LoadedTools:   make(map[string]contextplane.LoadedToolRecord, len(node.tools)),
		DeferredTools: make(map[string]contextplane.DeferredToolRecord),
		MaxAgeTurns:   2,
		MaxResultRefs: 32,
	}
	infos := make([]*schema.ToolInfo, 0, len(node.tools))
	for name, entry := range node.tools {
		state.LoadedTools[name] = contextplane.LoadedToolRecord{Name: name, LoadSource: "test"}
		info, err := entry.Tool.Info(context.Background())
		if err != nil {
			continue
		}
		if info != nil {
			infos = append(infos, info)
		}
	}
	return contextplane.WithToolLifecycleContext(ctx, ledger, state, nil, infos)
}
