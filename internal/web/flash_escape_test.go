package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// flashEscapeSlips finds a flash escaped twice. In this package an identifier named
// flash always holds a value encodeFlash already escaped, and encodeFlash(...) itself
// returns one. Escaping either again, with url.QueryEscape or with url.Values, turns
// the message into text the page cannot read back.
func flashEscapeSlips(fset *token.FileSet, file *ast.File) []token.Pos {
	var slips []token.Pos
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if isPkgSelector(node.Fun, "url", "QueryEscape") && len(node.Args) == 1 && isEncodedFlash(node.Args[0]) {
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
					if isEncodedFlash(v) {
						slips = append(slips, v.Pos())
					}
				}
			}
		}
		return true
	})
	return slips
}

// isEncodedFlash is true for a value that already went through encodeFlash: the call
// itself, or an identifier named flash, which by the package's convention holds one.
func isEncodedFlash(expr ast.Expr) bool {
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name == "flash"
	}
	return isCallNamed(expr, "encodeFlash")
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

// The package's own sources must never escape a flash twice. The check names the file
// and the line of each place.
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
				t.Errorf("%s: flash escaped twice; the flash arrives escaped, route it through the redirect as it is", fset.Position(pos))
			}
		}
	}
}

// The detector itself is checked on each shape, so a silent miss cannot pass. The sample
// holds the two shapes of the double escape (a call and a variable), the plain
// concatenation and the raw code, which are right.
func TestFlashEscapeDetectorFindsEveryDoubleEscape(t *testing.T) {
	src := `package web
import "net/url"
func a() string { return url.QueryEscape(encodeFlash(true, "x")) }
func b() url.Values { return url.Values{"flash": {encodeFlash(false, "y")}} }
func c(flash string) string { return "/x?flash=" + url.QueryEscape(flash) }
func d() string { flash := encodeFlash(true, "z"); return "/x?flash=" + flash }
func e() string { return "/x?flash=" + encodeFlash(true, "w") }
func f() url.Values { return url.Values{"flash": {flashCode(false, "v")}} }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", src, 0)
	if err != nil {
		t.Fatalf("parse sample: %v", err)
	}
	if got := len(flashEscapeSlips(fset, file)); got != 3 {
		t.Fatalf("detector found %d double escapes in the sample, want 3 (QueryEscape of encodeFlash, url.Values of encodeFlash, QueryEscape of a flash)", got)
	}
}
