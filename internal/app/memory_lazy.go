package app

import (
	"context"
	"sync"

	"github.com/ycvk/acorn/internal/memorymodule"
)

// lazySemanticIndex defers FAISS/Bleve construction until the first real use, so
// container startup (acorn pair / doctor / serve) is never blocked by embedding
// config or FAISS availability. It caches only a *successful* construction: a
// transient or canceled first call (e.g. a canceled request context, a momentary
// I/O error) is surfaced but not memoized, so the next call retries instead of
// poisoning semantic search for the whole process lifetime.
type lazySemanticIndex struct {
	build func(context.Context) (memorymodule.SemanticIndex, error)
	mu    sync.Mutex
	index memorymodule.SemanticIndex
}

func newLazySemanticIndex(build func(context.Context) (memorymodule.SemanticIndex, error)) *lazySemanticIndex {
	return &lazySemanticIndex{build: build}
}

func (l *lazySemanticIndex) resolve(ctx context.Context) (memorymodule.SemanticIndex, error) {
	// Short-circuit an already-canceled context before doing (or caching) work.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.index != nil {
		return l.index, nil
	}
	index, err := l.build(ctx)
	if err != nil {
		return nil, err // not cached — next call retries
	}
	l.index = index
	return l.index, nil
}

func (l *lazySemanticIndex) Rebuild(ctx context.Context, req memorymodule.SemanticRebuildRequest) (*memorymodule.SemanticRebuildResult, error) {
	index, err := l.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return index.Rebuild(ctx, req)
}

func (l *lazySemanticIndex) Search(ctx context.Context, req memorymodule.SemanticSearchRequest) (*memorymodule.SemanticSearchResult, error) {
	index, err := l.resolve(ctx)
	if err != nil {
		return nil, err
	}
	return index.Search(ctx, req)
}

func (l *lazySemanticIndex) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.index != nil {
		return l.index.Close()
	}
	return nil
}

// lazyEmbedder defers embedder construction until the first Embed call, caching
// only a successful build (same retry-on-failure policy as lazySemanticIndex).
type lazyEmbedder struct {
	build func() (memorymodule.Embedder, error)
	mu    sync.Mutex
	emb   memorymodule.Embedder
}

func newLazyEmbedder(build func() (memorymodule.Embedder, error)) *lazyEmbedder {
	return &lazyEmbedder{build: build}
}

func (l *lazyEmbedder) resolve() (memorymodule.Embedder, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.emb != nil {
		return l.emb, nil
	}
	emb, err := l.build()
	if err != nil {
		return nil, err // not cached — next call retries
	}
	l.emb = emb
	return l.emb, nil
}

func (l *lazyEmbedder) Embed(ctx context.Context, req memorymodule.EmbedRequest) (*memorymodule.EmbedResult, error) {
	emb, err := l.resolve()
	if err != nil {
		return nil, err
	}
	return emb.Embed(ctx, req)
}
