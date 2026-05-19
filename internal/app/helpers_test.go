package app

import (
	"path/filepath"
	"testing"

	storesqlite "github.com/ycvk/acorn/internal/store/sqlite"
)

func openTestStore(t *testing.T) *storesqlite.Store {
	t.Helper()
	store, err := storesqlite.Open(filepath.Join(t.TempDir(), ".acorn"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
