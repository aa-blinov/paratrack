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
		if !hasOwnerAuthorizationGuard(fset, function.Body) {
			return fmt.Errorf("%s %s must return model.ErrForbidden when caller or target differs from the authenticated owner", method.source, method.name)
		}
	}
	return nil
}

func hasOwnerAuthorizationGuard(fset *token.FileSet, body *ast.BlockStmt) bool {
	guarded := false
	ast.Inspect(body, func(node ast.Node) bool {
		statement, ok := node.(*ast.IfStmt)
		if !ok || statement.Body == nil {
			return true
		}
		comparisons := make(map[string]bool)
		ast.Inspect(statement.Cond, func(conditionNode ast.Node) bool {
			comparison, ok := conditionNode.(*ast.BinaryExpr)
			if !ok || comparison.Op != token.NEQ {
				return true
			}
			left := formatExpression(fset, comparison.X)
			right := formatExpression(fset, comparison.Y)
			comparisons[left+" != "+right] = true
			return true
		})
		if !comparisons["request.CallerID != actorID(ctx)"] || !comparisons["request.UserID != request.CallerID"] {
			return true
		}
		ast.Inspect(statement.Body, func(branchNode ast.Node) bool {
			returnStatement, ok := branchNode.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			for _, result := range returnStatement.Results {
				if formatExpression(fset, result) == "model.ErrForbidden" {
					guarded = true
				}
			}
			return true
		})
		return true
	})
	return guarded
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
