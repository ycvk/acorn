package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDependencyDirectionNoCycle enforces the Clean Slim Layers dependency
// direction: packages may only import packages in the same or a strictly
// lower layer. This prevents architectural drift.
//
// Layer 0: core (domain types, contracts, ports, tool registry interfaces)
// Layer 1: (reserved — port/contract merged into core)
// Layer 2: store, memory, mcp, tools, workspace, webaccess, skills, config
// Layer 3: runtime (executor, per-run assembly, session, masking, auto-compact)
// Layer 4: api
// Layer 5: wire
// Layer 6: cli

var layerRank = map[string]int{
	"core":      0,
	"config":    2,
	"store":     2,
	"memory":    2,
	"mcp":       2,
	"tools":     2,
	"workspace": 2,
	"webaccess": 2,
	"skills":    2,
	"runtime":   3,
	"api":       4,
	"wire":      5,
	"cli":       6,
}

func layerForPkg(internalPkg string) int {
	parts := strings.Split(internalPkg, "/")
	base := parts[len(parts)-1]
	if rank, ok := layerRank[base]; ok {
		return rank
	}
	if rank, ok := layerRank[parts[0]]; ok {
		return rank
	}
	return -1
}

func TestDependencyDirectionNoCycle(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		dir := filepath.Dir(rel)
		if dir == "." {
			return nil
		}
		importerPkg := strings.ReplaceAll(dir, string(filepath.Separator), "/")
		importerLayer := layerForPkg(importerPkg)
		if importerLayer < 0 {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Errorf("parse %s: %v", path, err)
			return nil
		}

		for _, imp := range file.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(impPath, "github.com/ycvk/acorn/internal/") {
				continue
			}
			internalPkg := strings.TrimPrefix(impPath, "github.com/ycvk/acorn/internal/")
			importedLayer := layerForPkg(internalPkg)
			if importedLayer < 0 {
				continue
			}
			if importedLayer > importerLayer {
				t.Errorf("dependency direction violation: %s (layer %d) imports %s (layer %d) — packages may only import same or lower layers",
					importerPkg, importerLayer, internalPkg, importedLayer)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// TestNoDirectStoreImportOutsideWire checks that packages outside wire/
// do not import internal/store directly. Sentinel errors are now in core,
// and ArtifactService is accessed via the core.ArtifactService interface.
func TestNoDirectStoreImportOutsideWire(t *testing.T) {
	root := filepath.Join("..", "..", "internal")
	violations := []string{}
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		pkgDir := filepath.Dir(rel)
		if pkgDir == "store" || pkgDir == "wire" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), `"github.com/ycvk/acorn/internal/store"`) {
			violations = append(violations, rel)
		}
		return nil
	})
	if len(violations) > 0 {
		t.Fatalf("internal/store imported outside wire/ by: %s", strings.Join(violations, ", "))
	}
}
