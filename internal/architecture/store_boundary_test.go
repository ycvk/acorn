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
	"internal/app/container.go":           {},
	"internal/app/container_bootstrap.go": {},
}

func TestSQLiteStoreImportsStayBehindCompositionRoot(t *testing.T) {
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
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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

	if len(offenders) > 0 {
		t.Fatalf("sqlite store imported outside composition root:\n%s", strings.Join(offenders, "\n"))
	}
}
