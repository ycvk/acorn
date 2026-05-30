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
	"internal/app/artifact_projection.go",
	"internal/app/client_service_event.go",
	"internal/web/dto_artifact.go",
	"internal/web/dto_decision.go",
	"internal/web/dto_run_detail.go",
	"internal/web/server.go",
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
			if importPath == "github.com/ycvk/acorn/internal/runtime" || importPath == "github.com/ycvk/acorn/internal/runtime/api" {
				offenders = append(offenders, rel+" imports "+importPath)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("client-facing projection files must not expose runtime-owned types:\n%s", strings.Join(offenders, "\n"))
	}
}
