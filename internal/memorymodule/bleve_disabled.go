//go:build !bleve_faiss || !vectors || !cgo

package memorymodule

import (
	"context"
	"errors"
)

var ErrBleveFAISSSupportNotBuilt = errors.New("bleve faiss semantic index support is not built; rebuild with CGO_ENABLED=1 and -tags 'bleve_faiss vectors'")

func NewBleveSemanticIndex(ctx context.Context, cfg BleveSemanticIndexConfig) (SemanticIndex, error) {
	return nil, ErrBleveFAISSSupportNotBuilt
}
