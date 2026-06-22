package app

import (
	"context"
	"errors"

	"github.com/ycvk/acorn/internal/memorymodule"
)

type MemoryService struct {
	module memorymodule.Service
}

func NewMemoryService(module memorymodule.Service) (*MemoryService, error) {
	if module == nil {
		return nil, errors.New("memory module service is required")
	}
	return &MemoryService{module: module}, nil
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
