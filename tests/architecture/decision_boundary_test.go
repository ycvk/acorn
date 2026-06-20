package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// T-003 RED: the decision subsystem must be demoted to defaults-only — no
// intent classifier, no profile routes. These fail today and are made green
// by T-004.

var intentRoutingFuncs = map[string]struct{}{
	"detectIntent":        {},
	"routeForIntent":      {},
	"resolveProfileRoute": {},
}

var intentRoutingPattern = regexp.MustCompile(`(?i)\b\w*(intent|route|classify)\w*\b`)

func TestDecisionPackageHasNoIntentRouting(t *testing.T) {
	dir := filepath.Join("..", "..", "internal", "decision")
	fset := token.NewFileSet()
	var offenders []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, pErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if pErr != nil {
			return pErr
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if _, bad := intentRoutingFuncs[fn.Name.Name]; bad {
				offenders = append(offenders, structRelFromRoot(t, path)+": "+fn.Name.Name)
			} else if intentRoutingPattern.MatchString(fn.Name.Name) {
				offenders = append(offenders, structRelFromRoot(t, path)+": "+fn.Name.Name+" (matches intent/route/classify pattern)")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk decision package: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("decision package must not contain intent-routing funcs:\n%s", strings.Join(offenders, "\n"))
	}
}

func TestDecisionMdHasNoAcornRoutes(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "decision.md"))
	if err != nil {
		t.Fatalf("read decision.md: %v", err)
	}
	if regexp.MustCompile(`(?i)acorn[-_]?routes`).MatchString(string(body)) {
		t.Fatalf("decision.md must not carry an acorn-routes block (decision is defaults-only)")
	}
}
