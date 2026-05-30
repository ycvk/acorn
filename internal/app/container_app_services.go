package app

import (
	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memorymodule"
	mcpprovider "github.com/ycvk/acorn/internal/providers/mcp"
)

func buildContainerAppServices(cfg *config.Config, store containerAppStore, deps *containerRuntimeDeps) (*Container, error) {
	container := &Container{
		cfg:           cfg,
		runnerFactory: deps.runnerFactory,
		runController: deps.runController,
	}

	container.runResume = NewRunResumeService(store).WithResume(deps.executors, store)
	checkpoints, err := NewWorkingCheckpointService(deps.checkpointService)
	if err != nil {
		return nil, err
	}
	container.checkpoints = checkpoints
	container.skills = NewSkillService(cfg, deps.loader)
	workspaceRoot := ""
	if deps.ws != nil {
		workspaceRoot = deps.ws.Root()
	}
	container.client = BuildClientService(store, deps.executors, deps.runController, workspaceRoot)
	container.pendingAction = NewPendingActionService(store)
	container.profiles = deps.decisionProfileService

	memoryService, err := NewMemoryService(deps.memoryModule, MemoryServiceSemanticOptions{
		Index:      deps.semanticIndex,
		Embedder:   deps.semanticEmbedder,
		Model:      cfg.Memory.Semantic.Embedding.Model,
		Dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
		BatchSize:  cfg.Memory.Semantic.Embedding.BatchSize,
		Schema:     memorymodule.SemanticSchemaMemoryRecordsV1,
		IndexName:  cfg.Memory.Semantic.Bleve.IndexName,
	})
	if err != nil {
		return nil, err
	}
	container.memory = memoryService

	container.capabilities = NewCapabilitiesService(cfg, container.skills, mcpprovider.Doctor, deps.runnerFactory)
	container.deviceAuth = NewDeviceAuthService(store)
	container.inbox = NewInboxService(store, container.capabilities)
	container.notifications = deps.notificationService

	return container, nil
}
