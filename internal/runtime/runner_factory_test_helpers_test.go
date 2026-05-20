package runtime

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memorymodule"
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
					APIKey:         "embedding-key",
					Dimensions:     1536,
					TimeoutSeconds: 30,
					BatchSize:      64,
				},
			},
		},
		Runtime: config.RuntimeConfig{StorageDir: filepath.Join(root, "state")},
		Web:     config.WebConfig{ListenAddr: "127.0.0.1:8080"},
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

func newRunnerFactory(t *testing.T, cfg *config.Config, store runnerFactoryStore, opts RunnerFactoryOptions) *RunnerFactory {
	t.Helper()
	if opts.MemoryModule == nil {
		opts.MemoryModule = newRunnerFactoryMemoryModule(t, cfg)
	}
	return NewRunnerFactory(cfg, store, opts)
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
