package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryChildProcessGetsAFilteredEnvironment walks the package source,
// build-tagged files included, and requires every exec.Command result to be
// stored in a variable whose Env is set to procenv.Child() in the same function.
func TestEveryChildProcessGetsAFilteredEnvironment(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checked += checkChildEnv(t, fset, fn.Body)
		}
	}
	if checked == 0 {
		t.Fatal("found no exec.Command call; the walk is not seeing the package source")
	}
}

func checkChildEnv(t *testing.T, fset *token.FileSet, body *ast.BlockStmt) int {
	t.Helper()
	bound := map[*ast.CallExpr]string{}
	filtered := map[string]bool{}
	var calls []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if isExecCommand(n) {
				calls = append(calls, n)
			}
		case *ast.AssignStmt:
			for i, rhs := range n.Rhs {
				if i >= len(n.Lhs) {
					break
				}
				if call, ok := rhs.(*ast.CallExpr); ok && isExecCommand(call) {
					if id, ok := n.Lhs[i].(*ast.Ident); ok {
						bound[call] = id.Name
					}
				}
				if sel, ok := n.Lhs[i].(*ast.SelectorExpr); ok && sel.Sel.Name == "Env" && isProcenvChild(rhs) {
					if id, ok := sel.X.(*ast.Ident); ok {
						filtered[id.Name] = true
					}
				}
			}
		}
		return true
	})
	for _, call := range calls {
		pos := fset.Position(call.Pos())
		name, ok := bound[call]
		if !ok {
			t.Errorf("%s: exec.Command result is used without setting Env = procenv.Child()", pos)
			continue
		}
		if !filtered[name] {
			t.Errorf("%s: %s.Env is never set to procenv.Child()", pos, name)
		}
	}
	return len(calls)
}

func isExecCommand(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "exec" && (sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext")
}

func isProcenvChild(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "procenv" && sel.Sel.Name == "Child"
}
