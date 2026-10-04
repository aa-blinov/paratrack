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
		if function == nil || !functionCalls(function, "requestedReportPerson") {
			return fmt.Errorf("internal/web/stats_view.go: %s must pass the manager-selected report person", name)
		}
	}
	requestedPerson := functionNamed(viewFile, "requestedReportPerson")
	if requestedPerson == nil || !functionCalls(requestedPerson, "canManage") {
		return fmt.Errorf("internal/web/stats_view.go: report person selection must be limited to managers")
	}

	builderFile, err := parser.ParseFile(fset, "internal/reports/builder.go", nil, 0)
	if err != nil {
		return fmt.Errorf("parse report builder: %w", err)
	}
	for _, check := range []struct{ function, scope string }{
		{function: "BuildStats", scope: "loadPersonScope"},
		{function: "BuildGraph", scope: "resolvePersonScope"},
	} {
		function := functionNamed(builderFile, check.function)
		if function == nil || !functionCalls(function, check.scope) {
			return fmt.Errorf("internal/reports/builder.go: %s must resolve report member scope in the workflow", check.function)
		}
	}
	resolver := functionNamed(builderFile, "resolvePersonScope")
	if resolver == nil || !functionCalls(resolver, "loadPersonScope") {
		return fmt.Errorf("internal/reports/builder.go: graph person scope must validate membership")
	}
	scope := functionNamed(builderFile, "loadPersonScope")
	if scope == nil || !functionCalls(scope, "Members") || !functionCalls(scope, "WithScope") {
		return fmt.Errorf("internal/reports/builder.go: report scope must validate workspace membership before applying user scope")
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
