package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/events"
)

func (f *RunnerFactory) validateRunMode(mode events.OrchestrationMode) error {
	if err := f.validateRunWorkspace(mode); err != nil {
		return err
	}
	return validateOrchestrationMode(mode)
}

func (f *RunnerFactory) validateRunWorkspace(mode events.OrchestrationMode) error {
	if mode == events.ModeDirectResponse {
		return nil
	}
	if f.deps.Workspace == nil {
		return errors.New("workspace contract is not initialized")
	}
	return nil
}

func validateOrchestrationMode(mode events.OrchestrationMode) error {
	switch mode {
	case events.ModeDirectResponse:
		return nil
	default:
		return fmt.Errorf("unsupported orchestration mode %q", mode)
	}
}

func (f *RunnerFactory) registerRunForBuild(req RunnerBuildRequest) (func(), error) {
	rc := &RunContext{RunID: req.RunID, ParentID: strings.TrimSpace(req.ParentRunID)}
	if err := f.registry.Register(rc); err != nil {
		return nil, fmt.Errorf("register run context: %w", err)
	}
	f.setCurrentRunID(req.RunID)
	return func() {
		f.registry.Clear(req.RunID)
		f.ClearCurrentRunID(req.RunID)
	}, nil
}

func (f *RunnerFactory) buildRunPrerequisites(ctx context.Context, req RunnerBuildRequest) (einomodel.BaseChatModel, *capabilityAssembly, error) {
	chatModel, err := f.buildRunChatModel(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	capabilityAssembly, err := f.buildRunCapabilityAssembly(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	return chatModel, capabilityAssembly, nil
}

func (f *RunnerFactory) assembleRunnerByMode(ctx context.Context, req RunnerBuildRequest, mode events.OrchestrationMode, chatModel einomodel.BaseChatModel, capabilityAssembly *capabilityAssembly) (*ActiveRunner, error) {
	return f.newDirectResponseRunner(ctx, req, chatModel, capabilityAssembly)
}
