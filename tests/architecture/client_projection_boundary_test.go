package architecture_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// clientProjectionBoundaryFiles are the /v1 client-facing projection and DTO
// files (formerly internal/clientevents, now merged into internal/api). They
// must not import internal/runtime — runtime-owned types must not leak into
// the client wire contract.
var clientProjectionBoundaryFiles = []string{
	"internal/api/projection.go",
	"internal/api/projection_helpers.go",
	"internal/api/event_service.go",
	"internal/api/thread_service.go",
	"internal/api/dto_run.go",
	"internal/api/dto_pending.go",
	"internal/api/dto_system.go",
	"internal/api/converter.go",
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
			if importPath == "github.com/ycvk/acorn/internal/runtime" {
				offenders = append(offenders, rel+" imports "+importPath)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("client-facing projection files must not expose runtime-owned types:\n%s", strings.Join(offenders, "\n"))
	}
}
