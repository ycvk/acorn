package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ycvk/acorn/internal/memorymodule"
)

type stubSemanticIndex struct{}

func (stubSemanticIndex) Rebuild(context.Context, memorymodule.SemanticRebuildRequest) (*memorymodule.SemanticRebuildResult, error) {
	return &memorymodule.SemanticRebuildResult{}, nil
}

func (stubSemanticIndex) Search(context.Context, memorymodule.SemanticSearchRequest) (*memorymodule.SemanticSearchResult, error) {
	return &memorymodule.SemanticSearchResult{}, nil
}

func (stubSemanticIndex) Close() error { return nil }

type stubEmbedder struct{}

func (stubEmbedder) Embed(context.Context, memorymodule.EmbedRequest) (*memorymodule.EmbedResult, error) {
	return &memorymodule.EmbedResult{}, nil
}

func TestLazySemanticIndexRetriesAfterFailure(t *testing.T) {
	calls := 0
	li := newLazySemanticIndex(func(context.Context) (memorymodule.SemanticIndex, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("transient boom")
		}
		return stubSemanticIndex{}, nil
	})

	if _, err := li.Search(context.Background(), memorymodule.SemanticSearchRequest{}); err == nil {
		t.Fatal("first Search should surface the build failure")
	}
	if _, err := li.Search(context.Background(), memorymodule.SemanticSearchRequest{}); err != nil {
		t.Fatalf("second Search should retry and succeed, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("build calls = %d, want 2 (failure must not be cached)", calls)
	}
}

func TestLazySemanticIndexShortCircuitsCanceledContext(t *testing.T) {
	calls := 0
	li := newLazySemanticIndex(func(context.Context) (memorymodule.SemanticIndex, error) {
		calls++
		return stubSemanticIndex{}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := li.Search(ctx, memorymodule.SemanticSearchRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ctx should short-circuit, got %v", err)
	}
	if calls != 0 {
		t.Fatalf("build must not run for a canceled ctx, calls = %d", calls)
	}
}

func TestLazyEmbedderRetriesAfterFailure(t *testing.T) {
	calls := 0
	le := newLazyEmbedder(func() (memorymodule.Embedder, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("transient boom")
		}
		return stubEmbedder{}, nil
	})

	if _, err := le.Embed(context.Background(), memorymodule.EmbedRequest{}); err == nil {
		t.Fatal("first Embed should surface the build failure")
	}
	if _, err := le.Embed(context.Background(), memorymodule.EmbedRequest{}); err != nil {
		t.Fatalf("second Embed should retry and succeed, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("build calls = %d, want 2 (failure must not be cached)", calls)
	}
}
