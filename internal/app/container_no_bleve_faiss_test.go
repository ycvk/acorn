//go:build !bleve_faiss || !vectors || !cgo

package app

import (
	"context"
	"errors"
	"testing"

	"github.com/ycvk/acorn/internal/memorymodule"
)

func TestNewContainerRequiresBleveFAISSSupport(t *testing.T) {
	_, err := NewContainer(context.Background(), testContainerConfig(t))
	if !errors.Is(err, memorymodule.ErrBleveFAISSSupportNotBuilt) {
		t.Fatalf("NewContainer error = %v, want ErrBleveFAISSSupportNotBuilt", err)
	}
}
