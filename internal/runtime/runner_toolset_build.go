package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/ycvk/acorn/internal/orchestration"
	"github.com/ycvk/acorn/internal/runtime/toolset"
	"github.com/ycvk/acorn/internal/tooling"
	"github.com/ycvk/acorn/internal/tools"
)

type localToolset struct {
	catalog *tools.Catalog
	closers []io.Closer
}

func (f *RunnerFactory) buildToolset(
	ctx context.Context,
	sessionID string,
	childExec orchestration.ChildAgentExecutor,
	includePlanning bool,
	profile tooling.ToolProfile,
) (_ *toolset.Toolset, err error) {
	if err := f.validateToolsetDeps(); err != nil {
		return nil, err
	}
	var closers []io.Closer
	defer func() { closeToolsetOnErr(closers, &err) }()
	local, err := f.buildLocalToolset(childExec)
	if err != nil {
		return nil, err
	}
	closers = append(closers, local.closers...)
	aux, err := f.buildAuxTools(ctx, sessionID, includePlanning)
	if err != nil {
		return nil, err
	}
	catalog, err := assembleToolsetCatalog(ctx, f.deps.Config, local.catalog, aux, includePlanning)
	if err != nil {
		return nil, err
	}
	return toolset.NewToolset(catalog, profile, closers...), nil
}

func (f *RunnerFactory) validateToolsetDeps() error {
	if f == nil || f.deps.Config == nil {
		return errors.New("runner factory is not initialized")
	}
	if f.deps.Workspace == nil {
		return errors.New("workspace contract is not initialized")
	}
	if f.deps.ArtifactService == nil {
		return errors.New("artifact service is not initialized")
	}
	return nil
}

func (f *RunnerFactory) buildLocalToolset(childExec orchestration.ChildAgentExecutor) (localToolset, error) {
	var out localToolset
	services, err := f.buildToolsetWebServices()
	if err != nil {
		return out, err
	}
	out.catalog, out.closers, err = f.buildLocalCatalog(services, childExec)
	return out, err
}

func closeToolsetOnErr(closers []io.Closer, err *error) {
	if *err == nil {
		return
	}
	var closeErrs []error
	for i := len(closers) - 1; i >= 0; i-- {
		if closers[i] == nil {
			continue
		}
		if closeErr := closers[i].Close(); closeErr != nil {
			closeErrs = append(closeErrs, closeErr)
		}
	}
	if len(closeErrs) > 0 {
		*err = errors.Join(*err, fmt.Errorf("close toolset after build failure: %w", errors.Join(closeErrs...)))
	}
}
