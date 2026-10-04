package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

func checkReportPersonScope(fset *token.FileSet) error {
	viewFile, err := parser.ParseFile(fset, "internal/web/stats_view.go", nil, 0)
	if err != nil {
		return fmt.Errorf("parse report views: %w", err)
	}
	for _, name := range []string{"buildStatsData", "buildGraphData"} {
		function := functionNamed(viewFile, name)
		if function == nil || !functionCalls(function, "resolveReportPersonScope") {
			return fmt.Errorf("internal/web/stats_view.go: %s must use the shared report person scope", name)
		}
	}

	scopeFile, err := parser.ParseFile(fset, "internal/web/report_scope.go", nil, 0)
	if err != nil {
		return fmt.Errorf("parse report scope: %w", err)
	}
	resolver := functionNamed(scopeFile, "resolveReportPersonScope")
	if resolver == nil || !functionCalls(resolver, "applyPersonScope") || !managerGuardPrecedesMemberRead(resolver) {
		return fmt.Errorf("internal/web/report_scope.go: report person scope must load members and reject non-managers before applying the filter")
	}
	return nil
}

func functionNamed(file *ast.File, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name {
			return function
		}
	}
	return nil
}

func functionCalls(function *ast.FuncDecl, name string) bool {
	if function == nil || function.Body == nil {
		return false
	}
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch target := call.Fun.(type) {
		case *ast.Ident:
			found = found || target.Name == name
		case *ast.SelectorExpr:
			found = found || target.Sel.Name == name
		}
		return true
	})
	return found
}

func managerGuardPrecedesMemberRead(function *ast.FuncDecl) bool {
	if function == nil || function.Body == nil {
		return false
	}
	var guard token.Pos
	for _, statement := range function.Body.List {
		conditional, ok := statement.(*ast.IfStmt)
		if !ok || !isNegatedCall(conditional.Cond, "canManage") {
			continue
		}
		for _, branchStatement := range conditional.Body.List {
			if _, ok := branchStatement.(*ast.ReturnStmt); ok {
				guard = conditional.Pos()
				break
			}
		}
		if guard.IsValid() {
			break
		}
	}
	if !guard.IsValid() {
		return false
	}
	var memberRead token.Pos
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Members" && (!memberRead.IsValid() || call.Pos() < memberRead) {
			memberRead = call.Pos()
		}
		return true
	})
	return memberRead.IsValid() && guard < memberRead
}

func isNegatedCall(expression ast.Expr, name string) bool {
	negation, ok := expression.(*ast.UnaryExpr)
	if !ok || negation.Op != token.NOT {
		return false
	}
	call, ok := negation.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	identifier, ok := call.Fun.(*ast.Ident)
	return ok && identifier.Name == name
}
