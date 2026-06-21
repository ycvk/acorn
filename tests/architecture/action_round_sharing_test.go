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

// T-005 RED: doneCriteria #7 — the "call model → run tools → reactive-compact"
// envelope must be a single shared primitive (orchestration.RunActionRound) used
// by BOTH direct_response (agent_loop.RunOneIteration) and plan/single_agent
// (act_node action round). Neither consumer may inline ExecuteRound + reactive
// compact directly. Currently both inline it and RunActionRound does not exist.

const actionRoundSharedFunc = "RunActionRound"

func TestSharedActionRoundPrimitiveExists(t *testing.T) {
	dir := filepath.Join("..", "..", "internal", "orchestration")
	if !hasFuncDecl(t, dir, actionRoundSharedFunc) {
		t.Fatalf("orchestration package must export shared action-round primitive %q", actionRoundSharedFunc)
	}
}

func TestRunOneIterationUsesSharedActionRound(t *testing.T) {
	path := filepath.Join("..", "..", "internal", "orchestration", "agent_loop.go")
	calls := callsInFunc(t, path, "RunOneIteration")
	if calls[actionRoundSharedFunc] == 0 {
		t.Fatalf("RunOneIteration must call shared %s (not inline ExecuteRound+compact)", actionRoundSharedFunc)
	}
	if calls["ExecuteRound"] > 0 {
		t.Fatalf("RunOneIteration must not call ExecuteRound directly (use %s); found %d direct call(s)", actionRoundSharedFunc, calls["ExecuteRound"])
	}
}

func TestActNodeUsesSharedActionRound(t *testing.T) {
	// RunActionRound may live in act_node.go or act_node_invoke.go (split file)
	base := filepath.Join("..", "..", "internal", "runtime", "plan")
	files := []string{
		filepath.Join(base, "act_node.go"),
		filepath.Join(base, "act_node_invoke.go"),
	}
	totalCalls := 0
	totalExecuteRound := 0
	for _, path := range files {
		calls := callsInFile(t, path)
		totalCalls += calls[actionRoundSharedFunc]
		totalExecuteRound += calls["ExecuteRound"]
	}
	if totalCalls == 0 {
		t.Fatalf("act_node action round must call shared %s (not inline ExecuteRound+compact)", actionRoundSharedFunc)
	}
	if totalExecuteRound > 0 {
		t.Fatalf("act_node must not call ExecuteRound directly (use %s); found %d direct call(s)", actionRoundSharedFunc, totalExecuteRound)
	}
}

func hasFuncDecl(t *testing.T, dir, name string) bool {
	t.Helper()
	fset := token.NewFileSet()
	found := false
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !isGoSource(path) {
			return walkErr
		}
		file, pErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if pErr != nil {
			return pErr
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Name.Name == name {
				found = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return found
}

func callsInFunc(t *testing.T, path, fnName string) map[string]int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == fnName && fn.Body != nil {
			return countCalls(fn.Body)
		}
	}
	t.Fatalf("func %s not found in %s", fnName, path)
	return nil
}

func callsInFile(t *testing.T, path string) map[string]int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return countCalls(file)
}

func countCalls(node ast.Node) map[string]int {
	counts := map[string]int{}
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name := callName(call); name != "" {
			counts[name]++
		}
		return true
	})
	return counts
}

func callName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	}
	return ""
}

func isGoSource(path string) bool {
	return filepath.Ext(path) == ".go" && !strings.HasSuffix(path, "_test.go")
}
