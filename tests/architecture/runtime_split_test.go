package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-007: internal/runtime must split into responsibility subpackages beyond
// pre-existing ones. The runtime/tool and runtime/toolset subpackages were
// promoted to runtime root in the structural convergence refactor; the
// remaining new subpackages are orchestration/ and eventstream/. The
// structural_limits test enforces file<=800 on internal/runtime top-level dir.
var preExistingRuntimeSubpackages = map[string]bool{
	"api": true, "graph": true, "plan": true, "tooltest": true,
}

func TestRuntimeSplitIntoResponsibilitySubpackages(t *testing.T) {
	runtimeDir := filepath.Join("..", "..", "internal", "runtime")
	newSubpkgs := countNewRuntimeSubpackages(t, runtimeDir)
	if newSubpkgs < 1 {
		t.Fatalf("internal/runtime must split into >=1 new responsibility subpackage (beyond api/graph/plan/tool/tooltest); found %d", newSubpkgs)
	}
}

func countNewRuntimeSubpackages(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || preExistingRuntimeSubpackages[entry.Name()] {
			continue
		}
		if hasNonTestGoFiles(filepath.Join(dir, entry.Name())) {
			count++
		}
	}
	return count
}

func hasNonTestGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			return true
		}
	}
	return false
}
