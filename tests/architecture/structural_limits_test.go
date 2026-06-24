package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// structural limits enforced on refactor-owned directories:
//   - file <= structFileMaxLines LOC
//
// Import cycles are enforced by `go build ./...` (compiler fails on cycles).
var refactorOwnedDirs = []string{
	"internal/runtime",
	"internal/agent",
	"internal/agent/factextract",
	"internal/stream",
	"internal/tools",
	"internal/context",

	"internal/port",

	"internal/memory",
	"internal/wire",
	"internal/providers/mcp",
	"internal/api",
	"internal/config",
	"internal/workspace",
	"internal/skills",
	"internal/webaccess",
}

const structFileMaxLines = 800

func TestStructuralLimitsRefactorOwnedRegistry(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range refactorOwnedDirs {
		assertStructuralLimitsRecursive(t, filepath.Join(root, dir))
	}
}

func assertStructuralLimitsRecursive(t *testing.T, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") {
			return nil
		}
		rel := structRelFromRoot(t, path)
		assertStructuralLimitsFile(t, rel, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk dir %s: %v", dir, err)
	}
}

func assertStructuralLimitsFile(t *testing.T, rel string, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	lines := len(strings.Split(string(data), "\n"))
	if strings.HasSuffix(string(data), "\n") {
		lines-- // trailing newline produces an extra empty element from Split
	}
	if lines > structFileMaxLines {
		t.Errorf("%s: %d lines exceeds %d limit", rel, lines, structFileMaxLines)
	}
}

func structRelFromRoot(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(filepath.Join("..", ".."), path)
	if err != nil {
		t.Fatalf("rel path %s: %v", path, err)
	}
	return filepath.ToSlash(rel)
}
