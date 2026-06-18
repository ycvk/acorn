package app

import (
	"context"
	"sync"

	"github.com/ycvk/acorn/internal/memorymodule"
)

// lazySemanticIndex defers FAISS/Bleve construction until the first real use, so
// container startup (acorn pair / doctor / serve) is never blocked by embedding
// config or FAISS availability. Construction failure surfaces (fail-loud) at
// first use, not hidden — there is no silent degradation.
type lazySemanticIndex struct {
	build func(context.Context) (memorymodule.SemanticIndex, error)
	once  sync.Once
	index memorymodule.SemanticIndex
	err   error
}

func newLazySemanticIndex(build func(context.Context) (memorymodule.SemanticIndex, error)) *lazySemanticIndex {
	return &lazySemanticIndex{build: build}
}

func (l *lazySemanticIndex) resolve(ctx context.Context) (memorymodule.SemanticIndex, error) {
	l.once.Do(func() {
		l.index, l.err = l.build(ctx)
	})
	return l.index, l.err
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
	// Only close what was actually constructed; never trigger construction here.
	if l.index != nil {
		return l.index.Close()
	}
	return nil
}

// lazyEmbedder defers embedder construction until the first Embed call.
type lazyEmbedder struct {
	build func() (memorymodule.Embedder, error)
	once  sync.Once
	emb   memorymodule.Embedder
	err   error
}

func newLazyEmbedder(build func() (memorymodule.Embedder, error)) *lazyEmbedder {
	return &lazyEmbedder{build: build}
}

func (l *lazyEmbedder) Embed(ctx context.Context, req memorymodule.EmbedRequest) (*memorymodule.EmbedResult, error) {
	l.once.Do(func() {
		l.emb, l.err = l.build()
	})
	if l.err != nil {
		return nil, l.err
	}
	return l.emb.Embed(ctx, req)
}
