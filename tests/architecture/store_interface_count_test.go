package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// T-011 RED -> GREEN: doneCriteria #10 — consumer-owned store interfaces
// (Store/Port/Repository/Ledger) defined by CONSUMERS (internal/runtime +
// internal/app) to access the store must consolidate from 33 to <=6. The
// "consumer-owned" scope is the narrow ports consumers define as subsets of
// the concrete store; interfaces owned by the store package itself
// (ArtifactStore, ToolResultLedger) or by other infrastructure packages
// (contextplane, mcp, model, workingstate) are NOT consumer-owned — they are
// the canonical contracts consumers depend on. After P1-A inlining the
// duplicated narrow ports, runtime defines 2 (ExecutorStore,
// RunnerFactoryStore) and app defines 4 (containerRuntimeStore,
// containerAppStore, PendingActionCreateStore, skillSnapshotStore) = 6.

var storeInterfacePattern = regexp.MustCompile(`^type \w*(Store|Port|Repository|Ledger) interface`)

var consumerOwnedDirs = []string{
	filepath.Join("..", "..", "internal", "runtime"),
	filepath.Join("..", "..", "internal", "app"),
}

func TestConsumerStoreInterfaceCount(t *testing.T) {
	count := 0
	for _, dir := range consumerOwnedDirs {
		count += countTopLevelStoreInterfaces(t, dir)
	}
	const maxConsumerStoreInterfaces = 6
	if count > maxConsumerStoreInterfaces {
		t.Fatalf("consumer-owned store interfaces (Store/Port/Repository/Ledger) in internal/runtime + internal/app top-level must consolidate to <=%d; found %d", maxConsumerStoreInterfaces, count)
	}
}
func countTopLevelStoreInterfaces(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if storeInterfacePattern.MatchString(line) {
				count++
			}
		}
	}
	return count
}
