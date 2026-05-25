package runtime

import (
	"context"

	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/toolfactory"
	"github.com/ycvk/acorn/internal/tooling"
)

func (f *RunnerFactory) BuildServeToolset(ctx context.Context) (*toolfactory.Toolset, error) {
	return f.buildToolset(ctx, "", nil, false, tooling.ToolProfileServe)
}

type delegateTaskBridge struct{}

func (delegateTaskBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (delegateTaskBridge) CurrentSessionID(ctx context.Context) string {
	return SessionIDFromContext(ctx)
}

type artifactToolBridge struct{}

func (artifactToolBridge) CurrentRunID(ctx context.Context) string {
	return CurrentRunID(ctx)
}

func (artifactToolBridge) CurrentSessionID(ctx context.Context) string {
	return SessionIDFromContext(ctx)
}

func (artifactToolBridge) CurrentToolCallID(ctx context.Context) string {
	return toolAuditCallID(ctx)
}

func (f *RunnerFactory) buildRunToolset(ctx context.Context, sessionID string, childExec orchestration.ChildAgentExecutor) (*toolfactory.Toolset, error) {
	return f.buildToolset(ctx, sessionID, childExec, true, tooling.ToolProfileRun)
}
