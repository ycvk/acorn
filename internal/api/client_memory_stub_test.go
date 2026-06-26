package api

import (
	"context"

	mem "github.com/ycvk/acorn/internal/memory"
)

type clientMemoryStub struct {
	facts            []mem.Record
	skills           []mem.Record
	history          []mem.Record
	search           []mem.SearchItem
	factSelection    mem.RecordSelection
	skillSelection   mem.RecordSelection
	historySelection mem.RecordSelection
	searchReq        mem.SearchRequest
}

func (s *clientMemoryStub) ListFacts(_ context.Context, selection mem.RecordSelection) ([]mem.Record, error) {
	s.factSelection = selection
	return append([]mem.Record(nil), s.facts...), nil
}

func (s *clientMemoryStub) ListSkills(_ context.Context, selection mem.RecordSelection) ([]mem.Record, error) {
	s.skillSelection = selection
	return append([]mem.Record(nil), s.skills...), nil
}

func (s *clientMemoryStub) ListHistory(_ context.Context, selection mem.RecordSelection) ([]mem.Record, error) {
	s.historySelection = selection
	return append([]mem.Record(nil), s.history...), nil
}

func (s *clientMemoryStub) Search(_ context.Context, req mem.SearchRequest) (*mem.SearchResult, error) {
	s.searchReq = req
	return &mem.SearchResult{Items: append([]mem.SearchItem(nil), s.search...)}, nil
}

func (s *clientMemoryStub) Root() string { return "" }

func (s *clientMemoryStub) Prepare(context.Context, mem.PrepareRequest) (*mem.PrepareResult, error) {
	return &mem.PrepareResult{}, nil
}

func (s *clientMemoryStub) AppendHistory(context.Context, mem.HistoryEvent) error { return nil }

func (s *clientMemoryStub) PlanMemoryMutation(context.Context, mem.PlanMemoryMutationRequest) (*mem.MemoryMutationPlan, error) {
	return &mem.MemoryMutationPlan{}, nil
}

func (s *clientMemoryStub) ApplyMemoryMutation(context.Context, mem.PlanMemoryMutationRequest) (*mem.MemoryMutationResult, error) {
	return &mem.MemoryMutationResult{}, nil
}

func (s *clientMemoryStub) CreateFact(context.Context, mem.CreateFactRequest) (*mem.Record, error) {
	return &mem.Record{}, nil
}

func (s *clientMemoryStub) BuildMemoryInstruction(context.Context, string) (string, error) {
	return "", nil
}
