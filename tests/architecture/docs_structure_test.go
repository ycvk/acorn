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
	var missing, unreferenced []string
	// waiting is the invariant whose first test reference has not come yet.
	waiting := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "每条") {
			continue
		}
		// Invariant lines start with "- **"
		if strings.HasPrefix(trimmed, "- **") {
			if waiting != "" {
				unreferenced = append(unreferenced, waiting)
			}
			waiting, _, _ = strings.Cut(strings.TrimPrefix(trimmed, "- **"), "**")
			continue
		}
		// Reference lines start with "- `path`"
		if m := refPattern.FindStringSubmatch(line); m != nil {
			waiting = ""
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
	if waiting != "" {
		unreferenced = append(unreferenced, waiting)
	}
	if len(unreferenced) > 0 {
		t.Errorf("INVARIANTS.md has invariants without a test file reference:\n%s", strings.Join(unreferenced, "\n"))
	}
	if len(missing) > 0 {
		t.Errorf("INVARIANTS.md references test files that do not exist:\n%s", strings.Join(missing, "\n"))
	}
}
