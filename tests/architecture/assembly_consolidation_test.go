package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// T-009 RED: doneCriteria #9 — P1-B assembly consolidation.
//   1. internal/runtime must not have BOTH assembleToolContext AND
//      assembleDirectContext (they must merge into a single entry).
//   2. orchestration.BuildDirectResponse must call assembleTooling (the shared
//      tool/handler/instruction assembly helper used by BuildSingleAgent and
//      BuildPlanExecute), instead of inlining its own bespoke tool building.
// Both assertions fail against the pre-refactor code, making this a true RED.

func TestAssemblyContextConsolidated(t *testing.T) {
	runtimeDir := filepath.Join("..", "..", "internal", "runtime")
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, runtimeDir, nonTestFilter, 0)
	if err != nil {
		t.Fatalf("parse runtime dir: %v", err)
	}
	names := []string{"assembleToolContext", "assembleDirectContext"}
	found := map[string]int{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				fn, ok := n.(*ast.FuncDecl)
				if !ok {
					return true
				}
				for _, want := range names {
					if fn.Name.Name == want {
						found[want]++
					}
				}
				return true
			})
		}
	}
	if found["assembleToolContext"] > 0 && found["assembleDirectContext"] > 0 {
		t.Fatalf("internal/runtime must not define both assembleToolContext (%d) and assembleDirectContext (%d); merge into a single entry", found["assembleToolContext"], found["assembleDirectContext"])
	}
}

func TestBuildDirectResponseUsesAssembleTooling(t *testing.T) {
	builder := filepath.Join("..", "..", "internal", "orchestration", "direct_response_builder.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, builder, nil, 0)
	if err != nil {
		t.Fatalf("parse direct_response_builder.go: %v", err)
	}
	var buildDirectResponse *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "BuildDirectResponse" {
			continue
		}
		buildDirectResponse = fn
	}
	if buildDirectResponse == nil {
		t.Fatal("BuildDirectResponse not found in direct_response_builder.go")
	}
	if !funcBodyCalls(buildDirectResponse.Body, "assembleTooling") {
		t.Fatal("orchestration.BuildDirectResponse must call assembleTooling (the shared assembly helper) instead of inlining bespoke tool building")
	}
}

func funcBodyCalls(body *ast.BlockStmt, name string) bool {
	called := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch sel := call.Fun.(type) {
		case *ast.SelectorExpr:
			if sel.Sel.Name == name {
				called = true
			}
		case *ast.Ident:
			if sel.Name == name {
				called = true
			}
		}
		return !called
	})
	return called
}

func nonTestFilter(info fs.FileInfo) bool {
	return !strings.HasSuffix(info.Name(), "_test.go")
}
