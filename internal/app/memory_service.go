package app

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/memorymodule"
)

type MemoryService struct {
	module   memorymodule.Service
	semantic MemoryServiceSemanticOptions
}

type MemoryServiceSemanticOptions struct {
	Index      memorymodule.SemanticIndex
	Embedder   memorymodule.Embedder
	Model      string
	Dimensions int
	BatchSize  int
	Schema     string
	IndexName  string
}

func NewMemoryService(module memorymodule.Service, semantic MemoryServiceSemanticOptions) (*MemoryService, error) {
	if module == nil {
		return nil, errors.New("memory module service is required")
	}
	return &MemoryService{module: module, semantic: semantic}, nil
}

func (s *MemoryService) ListFacts(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error) {
	return s.module.ListFacts(ctx, selection)
}

func (s *MemoryService) ListSkills(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error) {
	return s.module.ListSkills(ctx, selection)
}

func (s *MemoryService) ListHistory(ctx context.Context, selection memorymodule.RecordSelection) ([]memorymodule.Record, error) {
	return s.module.ListHistory(ctx, selection)
}

func (s *MemoryService) Search(ctx context.Context, req memorymodule.SearchRequest) (*memorymodule.SearchResult, error) {
	return s.module.Search(ctx, req)
}

func (s *MemoryService) RebuildSemanticIndex(ctx context.Context) (*memorymodule.SemanticRebuildResult, error) {
	if s == nil || s.module == nil {
		return nil, errors.New("memory service is required")
	}
	return s.module.RebuildSemanticIndex(ctx, memorymodule.SemanticRebuildOptions{
		Index:      s.semantic.Index,
		Embedder:   s.semantic.Embedder,
		Model:      s.semantic.Model,
		Dimensions: s.semantic.Dimensions,
		BatchSize:  s.semantic.BatchSize,
		Schema:     s.semantic.Schema,
		IndexName:  s.semantic.IndexName,
	})
}

func (s *MemoryService) PlanMemoryMutation(ctx context.Context, req memorymodule.PlanMemoryMutationRequest) (*memorymodule.MemoryMutationPlan, error) {
	return s.module.PlanMemoryMutation(ctx, req)
}

func (s *MemoryService) CreateProcedure(ctx context.Context, req memorymodule.CreateProcedureRequest) (*memorymodule.ProcedureRecord, error) {
	return s.module.CreateProcedure(ctx, req)
}
