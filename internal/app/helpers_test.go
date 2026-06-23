package app

import (
	"path/filepath"
	"testing"

	"github.com/ycvk/acorn/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	store, err := store.Open(filepath.Join(t.TempDir(), ".acorn"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
