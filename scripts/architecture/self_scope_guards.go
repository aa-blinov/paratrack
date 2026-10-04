package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
)

// checkSelfScopedPersistenceWrites keeps owner-only database mutations tied
// to the authenticated actor carried by request context.
func checkSelfScopedPersistenceWrites(fset *token.FileSet) error {
	methods := []struct{ source, name string }{
		{"internal/db/preferences.go", "SetUserPrefs"},
		{"internal/db/auth.go", "UpdateUserName"},
		{"internal/db/auth.go", "UpdateUserPasswordIfHashMatches"},
		{"internal/db/tokens.go", "CreateAPIToken"},
		{"internal/db/tokens.go", "ListAPITokens"},
		{"internal/db/tokens.go", "DeleteAPIToken"},
		{"internal/db/push.go", "UpsertPushSubscription"},
		{"internal/db/push.go", "DeletePushSubscription"},
	}
	for _, method := range methods {
		file, err := parserParseFile(fset, method.source)
		if err != nil {
			return err
		}
		var function *ast.FuncDecl
		for _, declaration := range file.Decls {
			candidate, ok := declaration.(*ast.FuncDecl)
			if ok && candidate.Name.Name == method.name && candidate.Recv != nil {
				function = candidate
				break
			}
		}
		if function == nil {
			return fmt.Errorf("%s is missing owner-scoped persistence method %s", method.source, method.name)
		}
		comparisons := make(map[string]bool)
		ast.Inspect(function.Body, func(node ast.Node) bool {
			comparison, ok := node.(*ast.BinaryExpr)
			if !ok || comparison.Op != token.NEQ {
				return true
			}
			left := formatExpression(fset, comparison.X)
			right := formatExpression(fset, comparison.Y)
			comparisons[left+" != "+right] = true
			return true
		})
		for _, required := range []string{
			"request.CallerID != actorID(ctx)",
			"request.UserID != request.CallerID",
		} {
			if !comparisons[required] {
				return fmt.Errorf("%s %s must reject a caller who does not match the authenticated resource owner (%s)", method.source, method.name, required)
			}
		}
	}
	return nil
}

func parserParseFile(fset *token.FileSet, source string) (*ast.File, error) {
	file, err := parser.ParseFile(fset, source, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Clean(source), err)
	}
	return file, nil
}

func formatExpression(fset *token.FileSet, expression ast.Expr) string {
	var output bytes.Buffer
	if err := format.Node(&output, fset, expression); err != nil {
		return "<unprintable>"
	}
	return output.String()
}
