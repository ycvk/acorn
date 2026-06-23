package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/memory"
)

func newTestMemoryModule(t *testing.T) memory.Service {
	t.Helper()
	dir := t.TempDir()
	memoryRoot := filepath.Join(dir, "memory")
	for _, sub := range []string{"facts", "skills", "history"} {
		if err := os.MkdirAll(filepath.Join(memoryRoot, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	module, err := memory.NewLocalService(memory.Config{Root: memoryRoot})
	if err != nil {
		t.Fatalf("create local service: %v", err)
	}
	return module
}

func TestNewMemoryServiceNilModule(t *testing.T) {
	_, err := NewMemoryService(nil)
	if err == nil {
		t.Fatal("expected error for nil module, got nil")
	}
}

func TestNewMemoryServiceSuccess(t *testing.T) {
	module := newTestMemoryModule(t)
	svc, err := NewMemoryService(module)
	if err != nil {
		t.Fatalf("new memory service: %v", err)
	}
	if svc == nil {
		t.Fatal("service should not be nil")
	}
}

func TestMemoryServiceListFactsEmpty(t *testing.T) {
	module := newTestMemoryModule(t)
	svc, _ := NewMemoryService(module)

	facts, err := svc.ListFacts(context.Background(), memory.RecordSelection{})
	if err != nil {
		t.Fatalf("list facts: %v", err)
	}
	if len(facts) != 0 {
		t.Errorf("expected 0 facts in empty store, got %d", len(facts))
	}
}

func TestMemoryServiceListFactsAfterCreate(t *testing.T) {
	module := newTestMemoryModule(t)
	svc, _ := NewMemoryService(module)

	ctx := context.Background()
	if _, err := module.CreateFact(ctx, memory.CreateFactRequest{
		Title: "Go typing",
		Body:  "Go is statically typed",
		Tags:  []string{"language", "go"},
	}); err != nil {
		t.Fatalf("create fact: %v", err)
	}

	facts, err := svc.ListFacts(ctx, memory.RecordSelection{})
	if err != nil {
		t.Fatalf("list facts: %v", err)
	}
	if len(facts) != 1 {
		t.Fatalf("expected 1 fact, got %d", len(facts))
	}
}

func TestMemoryServiceListSkillsEmpty(t *testing.T) {
	module := newTestMemoryModule(t)
	svc, _ := NewMemoryService(module)

	skills, err := svc.ListSkills(context.Background(), memory.RecordSelection{})
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestMemoryServiceListHistoryEmpty(t *testing.T) {
	module := newTestMemoryModule(t)
	svc, _ := NewMemoryService(module)

	history, err := svc.ListHistory(context.Background(), memory.RecordSelection{})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("expected 0 history records, got %d", len(history))
	}
}

func TestMemoryServiceListHistoryAfterAppend(t *testing.T) {
	module := newTestMemoryModule(t)
	svc, _ := NewMemoryService(module)

	ctx := context.Background()
	if err := module.AppendHistory(ctx, memory.HistoryEvent{
		SessionID: "sess_1",
		RunID:     "run_1",
		Status:    "succeeded",
		Summary:   "test run",
	}); err != nil {
		t.Fatalf("append history: %v", err)
	}

	history, err := svc.ListHistory(ctx, memory.RecordSelection{})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(history))
	}
}

func TestMemoryServiceSearchNoEmbedding(t *testing.T) {
	module := newTestMemoryModule(t)
	svc, _ := NewMemoryService(module)

	ctx := context.Background()
	if _, err := module.CreateFact(ctx, memory.CreateFactRequest{
		Title: "Go typing",
		Body:  "Go is statically typed",
		Tags:  []string{"go"},
	}); err != nil {
		t.Fatalf("create fact: %v", err)
	}

	result, err := svc.Search(ctx, memory.SearchRequest{
		Query: "Go",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result == nil {
		t.Fatal("search result should not be nil")
	}
}

func TestMemoryServiceRoot(t *testing.T) {
	dir := t.TempDir()
	memoryRoot := filepath.Join(dir, "memory")
	for _, sub := range []string{"facts", "skills", "history"} {
		_ = os.MkdirAll(filepath.Join(memoryRoot, sub), 0o755)
	}
	module, err := memory.NewLocalService(memory.Config{Root: memoryRoot})
	if err != nil {
		t.Fatalf("create local service: %v", err)
	}
	svc, _ := NewMemoryService(module)

	root := svc.module.Root()
	if root != memoryRoot {
		t.Errorf("Root() = %q, want %q", root, memoryRoot)
	}
}

func TestNewMemoryServiceWithConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Runtime.StorageDir = dir

	module := newTestMemoryModule(t)
	svc, err := NewMemoryService(module)
	if err != nil {
		t.Fatalf("new memory service: %v", err)
	}
	if svc.module == nil {
		t.Error("module should not be nil")
	}
	_ = cfg
}
