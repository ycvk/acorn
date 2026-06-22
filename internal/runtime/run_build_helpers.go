package runtime

import (
	"context"
	"fmt"
	"strings"

	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/ycvk/acorn/internal/events"
)

func (f *RunnerFactory) registerRunForBuild(req RunnerBuildRequest) (func(), error) {
	rc := &RunContext{RunID: req.RunID, ParentID: strings.TrimSpace("")}
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
