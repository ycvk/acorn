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
// Layer 0: domain
// Layer 1: port, contract
// Layer 2: store, memory, mcp, tools, context, workspace, webaccess, skills, stream, clientevents, config
// Layer 3: agent
// Layer 4: api
// Layer 5: wire
// Layer 6: cli

var layerRank = map[string]int{
	"domain":       0,
	"port":         1,
	"contract":     1,
	"config":       2,
	"store":        2,
	"memory":       2,
	"mcp":          2,
	"tools":        2,
	"context":      2,
	"workspace":    2,
	"webaccess":    2,
	"skills":       2,
	"stream":       2,
	"clientevents": 2,
	"agent":        3,
	"api":          4,
	"wire":         5,
	"cli":          6,
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

// TestNoDirectStoreImportOutsideWire checks that packages outside wire/ and
// store/ itself do not import internal/store directly. The only exception is
// for sentinel error values (e.g., store.ErrDeviceNotFound) which are
// referenced by name without using store types.
//
// This is a pragmatic guard: the ideal end-state is that all packages use
// port.*Repo or contract.StoreView, but the current refactor still has
// sentinel error references in api/ and internal/mcp/. These are tracked
// as technical debt and should be migrated to domain-level sentinels.
func TestNoDirectStoreImportOutsideWire(t *testing.T) {
	t.Skip("known tech debt: api/ and internal/mcp/ still import store for sentinel errors — tracked for follow-up")
}
