package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInvariantsAnnotatedWithTests(t *testing.T) {
	invMD := filepath.Join("..", "..", "docs", "architecture", "INVARIANTS.md")
	data, err := os.ReadFile(invMD)
	if err != nil {
		t.Fatalf("read INVARIANTS.md: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	refPattern := regexp.MustCompile("^\\s+- `([^`]+)`")
	var missing []string
	var noRef bool
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "每条") {
			continue
		}
		// Invariant lines start with "- **"
		if strings.HasPrefix(trimmed, "- **") {
			noRef = true
			continue
		}
		// Reference lines start with "- `path`"
		if m := refPattern.FindStringSubmatch(line); m != nil {
			noRef = false
			refPath := m[1]
			// Skip placeholder references like "mobile/test/..."
			if strings.HasSuffix(refPath, "...") {
				continue
			}
			fullPath := filepath.Join("..", "..", refPath)
			if _, err := os.Stat(fullPath); err != nil {
				missing = append(missing, refPath)
			}
		}
	}
	if noRef {
		t.Error("INVARIANTS.md has an invariant without a test file reference")
	}
	if len(missing) > 0 {
		t.Errorf("INVARIANTS.md references test files that do not exist:\n%s", strings.Join(missing, "\n"))
	}
}
