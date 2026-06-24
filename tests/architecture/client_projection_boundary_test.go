package architecture_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

var clientProjectionBoundaryFiles = []string{
	"internal/clientevents/types.go",
	"internal/clientevents/projector.go",
	"internal/api/thread_service.go",
	"internal/api/event_service.go",
	"internal/api/dto_run.go",
	"internal/api/dto_pending.go",
	"internal/api/server.go",
}

func TestClientProjectionBoundaryDoesNotImportRuntimeTypes(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var offenders []string
	for _, rel := range clientProjectionBoundaryFiles {
		file, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse imports for %s: %v", rel, err)
		}
		for _, item := range file.Imports {
			importPath := strings.Trim(item.Path.Value, `"`)
			if importPath == "github.com/ycvk/acorn/internal/agent" || importPath == "github.com/ycvk/acorn/internal/agent/api" {
				offenders = append(offenders, rel+" imports "+importPath)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("client-facing projection files must not expose runtime-owned types:\n%s", strings.Join(offenders, "\n"))
	}
}

func TestAppServicesDoNotImportStreamOutsideRuntimeAdapter(t *testing.T) {
	root := filepath.Join("..", "..")
	matches, err := filepath.Glob(filepath.Join(root, "internal", "app", "*.go"))
	if err != nil {
		t.Fatalf("glob app files: %v", err)
	}

	fset := token.NewFileSet()
	var offenders []string
	for _, path := range matches {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatalf("relative path for %s: %v", path, err)
		}
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse imports for %s: %v", rel, err)
		}
		for _, item := range file.Imports {
			importPath := strings.Trim(item.Path.Value, `"`)
			if importPath == "github.com/ycvk/acorn/internal/stream" {
				offenders = append(offenders, rel+" imports "+importPath)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("app services must not depend on runtime stream payloads outside executor adapter:\n%s", strings.Join(offenders, "\n"))
	}
}
