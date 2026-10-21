package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// flashEscapeSlips finds a flash escaped twice: url.QueryEscape around encodeFlash,
// or encodeFlash placed in url.Values, which escapes its value again on Encode.
func flashEscapeSlips(fset *token.FileSet, file *ast.File) []token.Pos {
	var slips []token.Pos
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if isPkgSelector(node.Fun, "url", "QueryEscape") && len(node.Args) == 1 && isCallNamed(node.Args[0], "encodeFlash") {
				slips = append(slips, node.Pos())
			}
		case *ast.CompositeLit:
			if !isPkgSelector(node.Type, "url", "Values") {
				return true
			}
			for _, elt := range node.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok || !isStringKey(kv.Key, "flash") {
					continue
				}
				values, ok := kv.Value.(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, v := range values.Elts {
					if isCallNamed(v, "encodeFlash") {
						slips = append(slips, v.Pos())
					}
				}
			}
		}
		return true
	})
	return slips
}

func isPkgSelector(expr ast.Expr, pkg, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == pkg
}

func isCallNamed(expr ast.Expr, name string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == name
}

func isStringKey(expr ast.Expr, value string) bool {
	lit, ok := expr.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING && lit.Value == `"`+value+`"`
}

// The package's own sources must never escape a flash twice: that turns a message
// into text the page cannot read back. The check names the file and line.
func TestNoFlashIsEscapedTwice(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package sources: %v", err)
	}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, pos := range flashEscapeSlips(fset, file) {
				t.Errorf("%s: flash escaped twice; route it through encodeFlash only", fset.Position(pos))
			}
		}
	}
}

// The detector itself is checked on both shapes, so a silent miss cannot pass.
func TestFlashEscapeDetectorFindsBothDoubleEscapes(t *testing.T) {
	src := `package web
import "net/url"
func a() string { return url.QueryEscape(encodeFlash(true, "x")) }
func b() url.Values { return url.Values{"flash": {encodeFlash(false, "y")}} }
func c() string { return "/x?flash=" + encodeFlash(true, "z") }
func d() url.Values { return url.Values{"flash": {flashCode(false, "w")}} }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, 0)
	if err != nil {
		t.Fatalf("parse sample: %v", err)
	}
	if got := len(flashEscapeSlips(fset, file)); got != 2 {
		t.Fatalf("detector found %d double escapes in the sample, want 2 (the plain concatenation and the raw code are fine)", got)
	}
}
