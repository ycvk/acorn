package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// /until-done structural limits enforced on refactor-owned directories:
//   - file <= structFileMaxLines LOC
//   - function declaration <= structFuncMaxLines LOC
//   - control-flow nesting <= structNestingMaxDepth
//
// refactorOwnedDirs is populated by RED tasks as directories become
// refactor-owned. Empty at bootstrap so the baseline stays green; existing
// large files are NOT retroactively covered (out of refactor scope).
// Import cycles are enforced by `go build ./...` (compiler fails on cycles).
var refactorOwnedDirs = []string{
	"internal/runtime",
	"internal/runtime/toolset",
}

const (
	structFileMaxLines    = 200
	structFuncMaxLines    = 30
	structNestingMaxDepth = 3
)

func TestStructuralLimitsRefactorOwnedRegistry(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range refactorOwnedDirs {
		assertStructuralLimitsRecursive(t, filepath.Join(root, dir))
	}
}

func assertStructuralLimitsRecursive(t *testing.T, dir string) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nonTestFileFilter, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse dir %s: %v", dir, err)
	}
	for _, pkg := range pkgs {
		for fname, file := range pkg.Files {
			rel := structRelFromRoot(t, fname)
			assertStructuralLimitsFile(t, rel, fname)
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				assertFuncLimits(t, rel, fn, fset)
			}
		}
	}
}

func nonTestFileFilter(info fs.FileInfo) bool {
	return !strings.HasSuffix(info.Name(), "_test.go")
}

func assertStructuralLimitsFile(t *testing.T, rel string, path string) {
	t.Helper()
	// Use actual file line count (not ast.File.End() which excludes trailing comments).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	lines := len(strings.Split(string(data), "\n"))
	if strings.HasSuffix(string(data), "\n") {
		lines-- // trailing newline produces an extra empty element from Split
	}
	if lines > structFileMaxLines {
		t.Errorf("%s: %d lines exceeds %d limit", rel, lines, structFileMaxLines)
	}
}

func structRelFromRoot(t *testing.T, path string) string {
	t.Helper()
	rel, err := filepath.Rel(filepath.Join("..", ".."), path)
	if err != nil {
		t.Fatalf("rel path %s: %v", path, err)
	}
	return filepath.ToSlash(rel)
}

func assertFuncLimits(t *testing.T, rel string, fn *ast.FuncDecl, fset *token.FileSet) {
	if fn.Body == nil {
		return
	}
	lines := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
	if lines > structFuncMaxLines {
		t.Errorf("%s:%d %s: %d lines exceeds %d limit",
			rel, fset.Position(fn.Pos()).Line, structFuncLabel(fn), lines, structFuncMaxLines)
	}
	if d := structFuncNesting(fn); d > structNestingMaxDepth {
		t.Errorf("%s:%d %s: nesting depth %d exceeds %d limit",
			rel, fset.Position(fn.Pos()).Line, structFuncLabel(fn), d, structNestingMaxDepth)
	}
}

func structFuncLabel(fn *ast.FuncDecl) string {
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		return structRecvType(fn) + "." + fn.Name.Name
	}
	return fn.Name.Name
}

func structRecvType(fn *ast.FuncDecl) string {
	switch t := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return "?"
}

// structFuncNesting returns the deepest control-flow nesting in a function.
// Depth increments when entering the body of if/for/range/switch/type-switch/
// select or a function literal. ast.Walk yields an enter Visit(node) and an
// exit Visit(nil) for every node whose enter returned a non-nil visitor, so a
// stack mirroring the DFS tracks nesting-adder enter/exit precisely.
func structFuncNesting(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}
	v := &structDepthVisitor{}
	ast.Walk(v, fn.Body)
	return v.max
}

type structDepthVisitor struct {
	stack []ast.Node
	depth int
	max   int
}

func (v *structDepthVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		if len(v.stack) > 0 {
			top := v.stack[len(v.stack)-1]
			v.stack = v.stack[:len(v.stack)-1]
			if structAddsNesting(top) {
				v.depth--
			}
		}
		return nil
	}
	v.stack = append(v.stack, node)
	if structAddsNesting(node) {
		v.depth++
		if v.depth > v.max {
			v.max = v.depth
		}
	}
	return v
}

func structAddsNesting(node ast.Node) bool {
	switch node.(type) {
	case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt,
		*ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.FuncLit:
		return true
	}
	return false
}
