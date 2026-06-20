package architecture_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// T-013 RED: doneCriteria #11 — docs/architecture must realign into:
//   1. ARCHITECTURE.md <=60 lines, purely descriptive (no prescriptive
//      tombstone assertions like "已删除/不存在/不能/唯一事实源").
//   2. INVARIANTS.md exists with each invariant annotated with a test file.
//   3. GLOSSARY.md exists.
// All three fail against the current 118-line prescriptive ARCHITECTURE.md
// with no INVARIANTS.md / GLOSSARY.md, making this a true RED.

var docTombstoneMarkers = []string{"已删除", "不存在", "不能", "唯一事实源"}

func TestArchitectureDocStructure(t *testing.T) {
	archDir := filepath.Join("..", "..", "docs", "architecture")
	archMD := filepath.Join(archDir, "ARCHITECTURE.md")
	data, err := os.ReadFile(archMD)
	if err != nil {
		t.Fatalf("read ARCHITECTURE.md: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > 60 {
		t.Errorf("ARCHITECTURE.md must be <=60 lines (purely descriptive); found %d", len(lines))
	}
	for i, line := range lines {
		for _, marker := range docTombstoneMarkers {
			if strings.Contains(line, marker) {
				t.Errorf("ARCHITECTURE.md:%d contains prescriptive tombstone marker %q (move to INVARIANTS.md): %s", i+1, marker, strings.TrimSpace(line))
			}
		}
	}
	if _, err := os.Stat(filepath.Join(archDir, "INVARIANTS.md")); err != nil {
		t.Errorf("docs/architecture/INVARIANTS.md must exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(archDir, "GLOSSARY.md")); err != nil {
		t.Errorf("docs/architecture/GLOSSARY.md must exist: %v", err)
	}
}

func TestInvariantsAnnotatedWithTests(t *testing.T) {
	invMD := filepath.Join("..", "..", "docs", "architecture", "INVARIANTS.md")
	data, err := os.ReadFile(invMD)
	if err != nil {
		t.Skipf("INVARIANTS.md not yet created: %v", err)
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
