package runtime

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memorymodule"
	"github.com/ycvk/acorn/internal/orchestration"
	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
)

func newRunnerFactoryMemoryTestContext(t *testing.T) (*storesqlite.Store, *config.Config) {
	t.Helper()

	root := t.TempDir()
	store, err := storesqlite.Open(filepath.Join(root, "state"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name:                "default",
				Model:               "gpt-test",
				BaseURL:             "https://example.invalid/v1",
				APIKey:              "chat-key",
				MaxCompletionTokens: 512,
				TimeoutSeconds:      30,
				Enabled:             true,
			},
		},
		Context: config.ContextConfig{
			WindowTokens:        200000,
			CompactMarginTokens: 13000,
			PreserveRecentTurns: 2,
			SummaryMaxTokens:    2048,
		},
		Memory: config.MemoryConfig{
			Search: config.MemorySearchConfig{
				MemoryContextTokenBudget: 2000,
			},
			Semantic: config.MemorySemanticConfig{
				Bleve: config.BleveSemanticConfig{
					IndexName: "memory_records",
				},
				Embedding: config.EmbeddingProviderConfig{
					Provider:       "openai_compatible",
					Model:          "text-embedding-3-small",
					BaseURL:        "https://api.openai.com/v1",
					APIKey:         "embedding-key",
					Dimensions:     1536,
					TimeoutSeconds: 30,
					BatchSize:      64,
				},
			},
		},
		Runtime: config.RuntimeConfig{StorageDir: filepath.Join(root, "state")},
		Web:     config.WebConfig{ListenAddr: "127.0.0.1:8080"},
		WebAccess: config.WebAccessConfig{
			UserAgent:        "Acorn test",
			TimeoutSeconds:   20,
			MaxResponseBytes: 10 * 1024 * 1024,
			Search: config.WebSearchConfig{
				Provider:       "tavily",
				TimeoutSeconds: 10,
				MaxResults:     10,
			},
		},
		Browser: config.BrowserConfig{
			Headless:              true,
			DefaultTimeoutSeconds: 20,
		},
		Agent: config.AgentConfig{
			Name:          "coordinator",
			Description:   "test",
			MaxIterations: 4,
		},
		Tools: config.ToolsConfig{
			Workspace:  config.WorkspaceToolConfig{RootDir: root},
			Mutation:   config.MutationToolConfig{RootDir: root},
			RunCommand: config.RunCommandToolConfig{Disabled: true, DefaultTimeout: 30, WorkDir: root},
		},
	}
	return store, cfg
}

func buildMCPFixtureServer(t *testing.T) string {
	t.Helper()
	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("resolve current file")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	fixtureDir := filepath.Join(repoRoot, "internal", "providers", "mcp", "testdata", "fixture_server")
	binary := filepath.Join(t.TempDir(), "fixture-mcp-server")
	cmd := exec.Command("go", "build", "-o", binary, fixtureDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build MCP fixture server: %v\n%s", err, string(output))
	}
	return binary
}

func newRunnerFactoryMemoryModule(t *testing.T, cfg *config.Config) memorymodule.Service {
	t.Helper()
	if cfg == nil {
		t.Fatal("config is nil")
	}
	service, err := memorymodule.NewLocalService(memorymodule.Config{Root: filepath.Join(cfg.Runtime.StorageDir, "memory")})
	if err != nil {
		t.Fatalf("NewLocalService: %v", err)
	}
	if err := service.EnsureLayout(t.Context()); err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	if err := service.BuildIndex(t.Context()); err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}
	if err := service.SetSemanticRuntime(memorymodule.SemanticRuntimeOptions{
		Index:      &runtimeTestSemanticIndex{},
		Embedder:   runtimeTestEmbedder{dimensions: cfg.Memory.Semantic.Embedding.Dimensions, model: cfg.Memory.Semantic.Embedding.Model},
		Model:      cfg.Memory.Semantic.Embedding.Model,
		Dimensions: cfg.Memory.Semantic.Embedding.Dimensions,
		BatchSize:  cfg.Memory.Semantic.Embedding.BatchSize,
		Schema:     memorymodule.SemanticSchemaMemoryRecordsV1,
		IndexName:  cfg.Memory.Semantic.Bleve.IndexName,
		Mode:       "hybrid",
	}); err != nil {
		t.Fatalf("SetSemanticRuntime: %v", err)
	}
	return service
}

func newRunnerFactory(t *testing.T, cfg *config.Config, store RunnerFactoryStore, opts RunnerFactoryOptions) *RunnerFactory {
	t.Helper()
	if opts.MemoryModule == nil {
		opts.MemoryModule = newRunnerFactoryMemoryModule(t, cfg)
	}
	if opts.ChildAgentExecutorFactory == nil {
		opts.ChildAgentExecutorFactory = NewSubagentExecutorFactory(cfg, store, nil)
	}
	factory, err := NewRunnerFactory(cfg, store, opts)
	if err != nil {
		t.Fatalf("NewRunnerFactory: %v", err)
	}
	return factory
}

func TestNewRunnerFactoryUsesInjectedContextPlane(t *testing.T) {
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	plane := &stubDeferredPlane{}

	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{
		ContextPlane: plane,
	})

	if factory.deps.ContextPlane != plane {
		t.Fatal("NewRunnerFactory did not retain injected context plane")
	}
}

func TestNewRunnerFactoryRequiresChildAgentExecutorFactory(t *testing.T) {
	store, cfg := newRunnerFactoryMemoryTestContext(t)

	_, err := NewRunnerFactory(cfg, store, RunnerFactoryOptions{
		MemoryModule: newRunnerFactoryMemoryModule(t, cfg),
	})
	if err == nil || !strings.Contains(err.Error(), "child agent executor factory is required") {
		t.Fatalf("NewRunnerFactory error = %v, want child agent executor factory requirement", err)
	}
}

func TestRunnerFactoryChildAgentFactoryReceivesRuntimeDeps(t *testing.T) {
	store, cfg := newRunnerFactoryMemoryTestContext(t)

	var got ChildAgentRuntimeDeps
	factory := newRunnerFactory(t, cfg, store, RunnerFactoryOptions{
		ChildAgentExecutorFactory: func(deps ChildAgentRuntimeDeps) (orchestration.ChildAgentExecutor, error) {
			got = deps
			return stubChildAgentExecutor{}, nil
		},
	})

	childExec, err := factory.newChildAgentExecutor()
	if err != nil {
		t.Fatalf("newChildAgentExecutor: %v", err)
	}
	if childExec == nil {
		t.Fatal("child executor is nil")
	}
	if got.RunRuntime != factory {
		t.Fatal("child agent factory did not receive the runner runtime facade")
	}
	if got.ParentDepth == nil {
		t.Fatal("child agent factory did not receive parent depth resolver")
	}
	if got.CreateChildWorkspace == nil {
		t.Fatal("child agent factory did not receive child workspace creator")
	}
	if got.RuntimeForWorkspace == nil {
		t.Fatal("child agent factory did not receive workspace runtime factory")
	}
}

func TestSubagentExecutorFactoryRequiresRuntimeDeps(t *testing.T) {
	store, cfg := newRunnerFactoryMemoryTestContext(t)
	factory := NewSubagentExecutorFactory(cfg, store, nil)

	_, err := factory(ChildAgentRuntimeDeps{})
	if err == nil || !strings.Contains(err.Error(), "run runtime is required") {
		t.Fatalf("factory error = %v, want run runtime requirement", err)
	}
}

type stubChildAgentExecutor struct{}

func (stubChildAgentExecutor) Execute(context.Context, orchestration.ChildAgentRequest) (*orchestration.ChildAgentResult, error) {
	return &orchestration.ChildAgentResult{}, nil
}

type runtimeTestSemanticIndex struct{}

func (i *runtimeTestSemanticIndex) Rebuild(context.Context, memorymodule.SemanticRebuildRequest) (*memorymodule.SemanticRebuildResult, error) {
	return nil, errors.New("runtime test semantic rebuild is not implemented")
}

func (i *runtimeTestSemanticIndex) Search(context.Context, memorymodule.SemanticSearchRequest) (*memorymodule.SemanticSearchResult, error) {
	return &memorymodule.SemanticSearchResult{}, nil
}

func (i *runtimeTestSemanticIndex) Close() error { return nil }

type runtimeTestEmbedder struct {
	dimensions int
	model      string
}

func (e runtimeTestEmbedder) Embed(_ context.Context, req memorymodule.EmbedRequest) (*memorymodule.EmbedResult, error) {
	vectors := make([]memorymodule.EmbeddingVector, 0, len(req.Inputs))
	for _, input := range req.Inputs {
		vectors = append(vectors, memorymodule.EmbeddingVector{
			Ref:    input.Ref,
			Values: make([]float32, e.dimensions),
		})
	}
	return &memorymodule.EmbedResult{Model: e.model, Dimensions: e.dimensions, Vectors: vectors}, nil
}
