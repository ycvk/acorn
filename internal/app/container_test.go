package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/decision"
	"github.com/ycvk/acorn/internal/memorymodule"
	notificationrouter "github.com/ycvk/acorn/internal/notifications"
	"github.com/ycvk/acorn/internal/runtime"
	"github.com/ycvk/acorn/internal/runtimehistory"
	"github.com/ycvk/acorn/internal/skills"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
	"github.com/ycvk/acorn/internal/workingstate"
)

func TestContainerBuildsCoreServices(t *testing.T) {
	container := testContainerFromDependencies(t, testContainerConfig(t))

	for name, service := range map[string]any{
		"chat":         container.Chat(),
		"run":          container.Run(),
		"resume":       container.Resume(),
		"sessionState": container.SessionState(),
		"capabilities": container.Capabilities(),
		"skills":       container.Skills(),
	} {
		if service == nil {
			t.Fatalf("expected %s service to be wired", name)
		}
	}
}

func TestContainerMCPServerNilWithoutAllowlist(t *testing.T) {
	cfg := testContainerConfig(t)
	// No serve.tools.allowlist configured
	container := testContainerFromDependencies(t, cfg)

	if container.MCPServer() != nil {
		t.Fatal("expected MCPServer() to return nil when allowlist is empty")
	}
}

func TestContainerMCPServerNonNilWithAllowlist(t *testing.T) {
	cfg := testContainerConfig(t)
	cfg.Serve = config.ServeConfig{
		Tools: config.ServeToolsConfig{
			Allowlist: []string{"read_file"},
		},
	}
	container := testContainerFromDependencies(t, cfg)

	if container.MCPServer() == nil {
		t.Fatal("expected MCPServer() to return non-nil when allowlist is configured")
	}
}

func testContainerFromDependencies(t *testing.T, cfg *config.Config) *Container {
	t.Helper()
	deps := testContainerDependencies(t, cfg)
	container, err := buildContainerFromDependencies(cfg, deps)
	if err != nil {
		_ = deps.store.Close()
		t.Fatalf("build container from dependencies: %v", err)
	}
	t.Cleanup(func() { _ = container.Close() })
	return container
}

func testContainerDependencies(t *testing.T, cfg *config.Config) *containerDependencies {
	t.Helper()
	store, err := storesqlite.Open(cfg.Runtime.StorageDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ws, err := cfg.Workspace()
	if err != nil {
		_ = store.Close()
		t.Fatalf("workspace: %v", err)
	}
	loader := skills.NewLoader(cfg)
	checkpointService := workingstate.NewService(store, 4000)
	sessionSummaryService := runtimehistory.NewSessionSummaryService(store, 2000)
	memoryModule, err := memorymodule.NewLocalService(memorymodule.Config{Root: filepath.Join(cfg.Runtime.StorageDir, "memory")})
	if err != nil {
		_ = store.Close()
		t.Fatalf("new memory module: %v", err)
	}
	if err := memoryModule.EnsureLayout(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("ensure memory layout: %v", err)
	}
	if err := memoryModule.BuildIndex(context.Background()); err != nil {
		_ = store.Close()
		t.Fatalf("build memory index: %v", err)
	}
	semanticIndex := &containerSemanticIndex{}
	semanticEmbedder := containerEmbedder{
		model:      cfg.Memory.Semantic.Embedding.Model,
		dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
	}
	if err := memoryModule.SetSemanticRuntime(memorymodule.SemanticRuntimeOptions{
		Index:      semanticIndex,
		Embedder:   semanticEmbedder,
		Model:      cfg.Memory.Semantic.Embedding.Model,
		Dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
		BatchSize:  cfg.Memory.Semantic.Embedding.BatchSize,
		Schema:     memorymodule.SemanticSchemaMemoryRecordsV1,
		IndexName:  cfg.Memory.Semantic.Bleve.IndexName,
		Mode:       "hybrid",
	}); err != nil {
		_ = store.Close()
		t.Fatalf("set semantic runtime: %v", err)
	}
	notificationService := NewNotificationService(store, notificationrouter.Router{})
	runnerFactory := runtime.NewRunnerFactory(cfg, store, runtime.RunnerFactoryOptions{
		Loader:                 loader,
		Workspace:              ws,
		DecisionProfileService: decision.NewProfileService(ws.Root()),
		CheckpointService:      checkpointService,
		SessionSummaryService:  sessionSummaryService,
		MemoryModule:           memoryModule,
		MCPPendingActionStore:  NewNotifyingPendingActionStore(store, notificationService),
	})
	runController := runtime.NewRunController()
	return &containerDependencies{
		store:             store,
		workspace:         ws,
		loader:            loader,
		decisionProfiles:  decision.NewProfileService(ws.Root()),
		checkpointService: checkpointService,
		memoryModule:      memoryModule,
		semanticIndex:     semanticIndex,
		semanticEmbedder:  semanticEmbedder,
		notifications:     notificationService,
		runnerFactory:     runnerFactory,
		runController:     runController,
		executors:         newExecutorFactory(cfg, store, runnerFactory, runController),
	}
}

func testContainerConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		Runtime: config.RuntimeConfig{
			StorageDir: filepath.Join(t.TempDir(), ".acorn"),
		},
		Web: config.WebConfig{ListenAddr: "127.0.0.1:8080"},
		Agent: config.AgentConfig{
			Name:          "coordinator",
			Description:   "test",
			MaxIterations: 4,
		},
		Providers: []config.ProviderConfig{{
			Enabled:             true,
			Name:                "openai",
			BaseURL:             "https://example.com/v1",
			APIKey:              "test-key",
			Model:               "gpt-test",
			MaxCompletionTokens: 512,
			TimeoutSeconds:      30,
		}},
		Context: config.ContextConfig{
			WindowTokens:        200000,
			CompactMarginTokens: 13000,
			PreserveRecentTurns: 3,
			SummaryMaxTokens:    2048,
		},
		Memory: config.MemoryConfig{
			Search: config.LayeredMemorySearchConfig{
				TokenBudget: 2000,
			},
			Semantic: config.MemorySemanticConfig{
				Bleve: config.BleveSemanticConfig{
					IndexName: "memory_records",
				},
				Embedding: config.EmbeddingProviderConfig{
					Provider:       "openai_compatible",
					Model:          "text-embedding-3-small",
					BaseURL:        "https://api.openai.com/v1",
					APIKey:         "test-key",
					Dimensions:     3,
					TimeoutSeconds: 30,
					BatchSize:      2,
				},
			},
		},
		Tools: config.ToolsConfig{
			Workspace: config.WorkspaceToolConfig{RootDir: "."},
			Mutation:  config.MutationToolConfig{RootDir: "."},
			RunCommand: config.RunCommandToolConfig{
				WorkDir: ".",
			},
		},
	}
}

type containerSemanticIndex struct{}

func (*containerSemanticIndex) Rebuild(context.Context, memorymodule.SemanticRebuildRequest) (*memorymodule.SemanticRebuildResult, error) {
	return &memorymodule.SemanticRebuildResult{}, nil
}

func (*containerSemanticIndex) Search(context.Context, memorymodule.SemanticSearchRequest) (*memorymodule.SemanticSearchResult, error) {
	return &memorymodule.SemanticSearchResult{}, nil
}

func (*containerSemanticIndex) Close() error {
	return nil
}

type containerEmbedder struct {
	model      string
	dimensions int
}

func (e containerEmbedder) Embed(_ context.Context, req memorymodule.EmbedRequest) (*memorymodule.EmbedResult, error) {
	result := &memorymodule.EmbedResult{
		Model:      e.model,
		Dimensions: e.dimensions,
		Vectors:    make([]memorymodule.EmbeddingVector, 0, len(req.Inputs)),
	}
	for _, input := range req.Inputs {
		result.Vectors = append(result.Vectors, memorymodule.EmbeddingVector{
			Ref:    input.Ref,
			Values: make([]float32, e.dimensions),
		})
	}
	return result, nil
}
