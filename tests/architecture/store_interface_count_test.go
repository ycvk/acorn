package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Consumer-owned store interfaces (Store/Port/Repository/Ledger) defined by
// CONSUMERS (internal/agent + internal/wire) must consolidate to <=4.

var storeInterfacePattern = regexp.MustCompile(`^type \w*(Store|Port|Repository|Ledger) interface`)

var consumerOwnedDirs = []string{
	filepath.Join("..", "..", "internal", "agent"),
	filepath.Join("..", "..", "internal", "wire"),
}

func TestConsumerStoreInterfaceCount(t *testing.T) {
	count := 0
	for _, dir := range consumerOwnedDirs {
		count += countTopLevelStoreInterfaces(t, dir)
	}
	const maxConsumerStoreInterfaces = 4
	if count > maxConsumerStoreInterfaces {
		t.Fatalf("consumer-owned store interfaces (Store/Port/Repository/Ledger) in internal/agent + internal/wire top-level must consolidate to <=%d; found %d", maxConsumerStoreInterfaces, count)
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
