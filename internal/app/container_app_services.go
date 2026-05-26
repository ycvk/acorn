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

	container.sessions = NewSessionService(store)
	container.trace = NewTraceService(store)
	container.sessionState = NewSessionStateService(cfg, store, container.trace)
	container.workbench = NewRuntimeWorkbenchService(RuntimeWorkbenchConfig{
		Workspace: deps.ws,
	}, store, container.trace)
	checkpoints, err := NewWorkingCheckpointService(deps.checkpointService)
	if err != nil {
		return nil, err
	}
	container.checkpoints = checkpoints
	container.skills = NewSkillService(cfg, deps.loader)
	container.chat = NewChatService(store, deps.executors)
	workspaceRoot := ""
	if deps.ws != nil {
		workspaceRoot = deps.ws.Root()
	}
	container.client = BuildClientService(store, deps.executors, workspaceRoot)
	container.pendingAction = NewPendingActionService(store)
	container.run = NewRunService(deps.executors, deps.runController)
	container.resume = NewResumeService(container.trace, deps.executors, store)
	container.decision = NewDecisionService(deps.decisionProfileService, store)

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
