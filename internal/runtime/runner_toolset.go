package runtime

import (
	"context"

	"github.com/ycvk/acorn/internal/orchestration"
	runtimeapi "github.com/ycvk/acorn/internal/runtime/api"
	"github.com/ycvk/acorn/internal/runtime/tool"
	"github.com/ycvk/acorn/internal/runtime/toolset"
	"github.com/ycvk/acorn/internal/tooling"
)

func (f *RunnerFactory) BuildServeToolset(ctx context.Context) (*toolset.Toolset, error) {
	return f.buildToolset(ctx, "", nil, false, tooling.ToolProfileServe)
}

type delegateTaskBridge struct{}

func (delegateTaskBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (delegateTaskBridge) CurrentSessionID(ctx context.Context) string {
	return runtimeapi.SessionIDFromContext(ctx)
}

type artifactToolBridge struct{}

func (artifactToolBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (artifactToolBridge) CurrentSessionID(ctx context.Context) string {
	return runtimeapi.SessionIDFromContext(ctx)
}

func (artifactToolBridge) CurrentToolCallID(ctx context.Context) string {
	return tool.ToolAuditCallID(ctx)
}

func (f *RunnerFactory) buildRunToolset(ctx context.Context, sessionID string, childExec orchestration.ChildAgentExecutor) (*toolset.Toolset, error) {
	return f.buildToolset(ctx, sessionID, childExec, true, tooling.ToolProfileRun)
}
