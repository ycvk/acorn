package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const sqliteImportPath = "github.com/ycvk/acorn/internal/store/sqlite"

var sqliteImportAllowlist = map[string]struct{}{
	"internal/app/container.go": {},
	// SQLite-backed integration tests intentionally exercise persisted runtime
	// behavior that unit fakes cannot validate.
	"internal/runtime/executor_finalization_test.go":       {},
	"internal/runtime/executor_run_e2e_test.go":            {},
	"internal/runtime/runner_factory_test_helpers_test.go": {},
	"internal/providers/mcp/elicitation_handler_test.go":   {},
	"internal/toolset/tools_test.go":                       {},
	// App/service integration tests validate persisted /v1 projections and
	// session state against the SQLite store contract.
	"internal/app/client_service_test.go":         {},
	"internal/app/helpers_test.go":                {},
	"internal/app/pending_action_service_test.go": {},
}

func TestSQLiteStoreImportsStayBehindCompositionRoot(t *testing.T) {
	offenders := scanSQLiteImports(t, false)
	if len(offenders) > 0 {
		t.Fatalf("sqlite store imported outside composition root:\n%s", strings.Join(offenders, "\n"))
	}
}

func TestSQLiteStoreImportsStayBehindCompositionRoot_InTests(t *testing.T) {
	offenders := scanSQLiteImports(t, true)
	if len(offenders) > 0 {
		t.Fatalf("sqlite store imported in tests outside composition root:\n%s", strings.Join(offenders, "\n"))
	}
}

func scanSQLiteImports(t *testing.T, includeTests bool) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	internalRoot := filepath.Join(root, "internal")
	fset := token.NewFileSet()
	var offenders []string

	if err := filepath.WalkDir(internalRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "sqlite" && strings.HasSuffix(filepath.ToSlash(path), "internal/store/sqlite") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if !includeTests && strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if includeTests && !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, ok := sqliteImportAllowlist[rel]; ok {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, item := range file.Imports {
			if strings.Trim(item.Path.Value, `"`) == sqliteImportPath {
				offenders = append(offenders, rel)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("scan internal package imports: %v", err)
	}
	return offenders
}
