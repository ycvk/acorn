package architecture_test

import (
	"bytes"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const bleveImportPath = "github.com/blevesearch/bleve/v2"

func TestProductionBleveImportsStayBuildTagged(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var offenders []string

	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "frontend", "mobile", "dist", "node_modules":
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
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, item := range file.Imports {
			importPath := strings.Trim(item.Path.Value, `"`)
			if importPath == bleveImportPath || strings.HasPrefix(importPath, bleveImportPath+"/") {
				if !allowedBleveImportFile(path, rel) {
					offenders = append(offenders, rel)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("scan production Bleve imports: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("production Bleve imports must stay confined to bleve_faiss,vectors,cgo build-tagged adapter files:\n%s", strings.Join(offenders, "\n"))
	}
}

func allowedBleveImportFile(path string, rel string) bool {
	// Allow any file matching bleve_index*.go pattern in memorymodule,
	// as long as it carries the bleve_faiss,vectors,cgo build tag.
	if !strings.HasPrefix(rel, "internal/memorymodule/bleve_index") || !strings.HasSuffix(rel, ".go") {
		return false
	}
	data, err := osReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte("//go:build bleve_faiss && vectors && cgo"))
}

var osReadFile = func(path string) ([]byte, error) {
	return os.ReadFile(path)
}
