package runtime

import (
	"context"
	"errors"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/tooling"
)

type Toolset struct {
	catalog *tooling.Catalog
	profile tooling.ToolProfile
	closers []toolsetCloser
}

type toolsetCloser interface {
	Close() error
}

func (t Toolset) All() []einotool.BaseTool {
	if t.catalog == nil {
		return nil
	}
	return t.catalog.ToolsForProfile(t.profile)
}

func (t Toolset) Catalog() *tooling.Catalog {
	return t.catalog
}

func (t *Toolset) Close() error {
	if t == nil {
		return nil
	}
	return closeToolsetClosers(t.closers)
}

func closeToolsetClosers(closers []toolsetCloser) error {
	var errs []error
	for i := len(closers) - 1; i >= 0; i-- {
		if closers[i] == nil {
			continue
		}
		if err := closers[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (f *RunnerFactory) BuildServeToolset(ctx context.Context) (*Toolset, error) {
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

func (f *RunnerFactory) buildRunToolset(ctx context.Context, sessionID string, childExec orchestration.ChildAgentExecutor) (*Toolset, error) {
	return f.buildToolset(ctx, sessionID, childExec, true, tooling.ToolProfileRun)
}
