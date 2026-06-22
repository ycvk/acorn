package app

import (
	"github.com/ycvk/acorn/internal/config"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
)

func buildContainerAppServices(cfg *config.Config, store containerAppStore, deps *containerRuntimeDeps) (*Container, error) {
	container := &Container{
		cfg:           cfg,
		runnerFactory: deps.runnerFactory,
		runController: deps.runController,
	}

	container.runResume = NewRunResumeService(store).WithResume(deps.executors)
	container.skills = NewSkillService(cfg, deps.loader)
	workspaceRoot := ""
	if deps.ws != nil {
		workspaceRoot = deps.ws.Root()
	}
	container.client = BuildClientService(store, deps.executors, deps.runController, workspaceRoot)
	container.pendingAction = NewPendingActionService(store)

	memoryService, err := NewMemoryService(deps.memoryModule)
	if err != nil {
		return nil, err
	}
	container.memory = memoryService

	container.capabilities = NewCapabilitiesService(cfg, container.skills, mcpprovider.Doctor, deps.runnerFactory)
	container.deviceAuth = NewDeviceAuthService(store)
	container.inbox = NewInboxService(store, container.capabilities)

	return container, nil
}
