package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestOwnerAuthorizationGuardRequiresRejectingBranch(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "rejects mismatched caller and target",
			body: `func write() {
				if request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
					return model.ErrForbidden
				}
			}`,
			want: true,
		},
		{
			name: "comparison outside rejecting branch",
			body: `func write() {
				_ = request.CallerID != actorID(ctx)
				if request.UserID != request.CallerID { return nil }
			}`,
		},
		{
			name: "mismatch branch does not reject",
			body: `func write() {
				if request.CallerID != actorID(ctx) || request.UserID != request.CallerID {
					logMismatch()
				}
			}`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "fixture.go", "package db\n"+testCase.body, 0)
			if err != nil {
				t.Fatal(err)
			}
			function := file.Decls[0].(*ast.FuncDecl)
			if got := hasOwnerAuthorizationGuard(fset, function.Body); got != testCase.want {
				t.Fatalf("hasOwnerAuthorizationGuard() = %v, want %v", got, testCase.want)
			}
		})
	}
}
