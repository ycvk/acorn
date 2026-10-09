package architecture_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The jsonschema tag splits on commas, so a description written as
// jsonschema:"description=a, b" reaches the model as "a". Descriptions must use
// the jsonschema_description tag, which keeps the whole text.
var descriptionInJSONSchemaTag = regexp.MustCompile(`jsonschema:"[^"]*description=`)

func TestToolParameterDescriptionsUseDedicatedTag(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if descriptionInJSONSchemaTag.MatchString(line) {
				violations = append(violations, fmt.Sprintf("%s:%d", filepath.ToSlash(path), i+1))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("use jsonschema_description for parameter descriptions:\n%s", strings.Join(violations, "\n"))
	}
}
