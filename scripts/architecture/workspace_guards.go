package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

func checkWorkflowWorkspaceGuardCoverage(fset *token.FileSet, parsed map[string]*ast.File, packages []string, contractFields map[string]map[string]bool) error {
	// These operations intentionally accept no active workspace: logout may
	// happen before workspace selection, and push enqueue is a best-effort
	// internal notification capability that silently ignores empty scope.
	exceptions := map[string]bool{
		"internal/auth/sessions.go:Logout":             true,
		"internal/auth/tokens.go:CreateAPIToken":       true,
		"internal/audit/service.go:RecordGlobal":       true,
		"internal/push/service.go:EnqueueNotification": true,
	}
	for _, packagePath := range packages {
		sources, err := filepath.Glob(filepath.Join(packagePath, "*.go"))
		if err != nil {
			return fmt.Errorf("list workflow sources in %s: %w", packagePath, err)
		}
		for _, source := range sources {
			if strings.HasSuffix(source, "_test.go") {
				continue
			}
			file := parsed[source]
			if file == nil {
				file, err = parser.ParseFile(fset, source, nil, 0)
				if err != nil {
					return fmt.Errorf("parse %s: %w", source, err)
				}
				parsed[source] = file
			}
			workspaceTypes := mergeWorkspaceStructFields(workspaceStructFields(file), contractFields)
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Recv == nil || !ast.IsExported(function.Name.Name) || !hasWorkspaceParameter(function, workspaceTypes) {
					continue
				}
				key := source + ":" + function.Name.Name
				if exceptions[key] {
					continue
				}
				if err := checkMethod(fset, source, function.Name.Name, file, contractFields); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type workspaceParameter struct {
	name      string
	fieldName string
}

func hasWorkspaceParameter(function *ast.FuncDecl, workspaceTypes map[string]map[string]bool) bool {
	_, ok := workspaceParameterFor(function, workspaceTypes)
	return ok
}

func workspaceParameterFor(function *ast.FuncDecl, workspaceTypes map[string]map[string]bool) (workspaceParameter, bool) {
	if function.Type.Params == nil {
		return workspaceParameter{}, false
	}
	for _, field := range function.Type.Params.List {
		for _, name := range field.Names {
			if name.Name == "teamID" || name.Name == "workspaceID" {
				return workspaceParameter{name: name.Name}, true
			}
		}
		parameterType := field.Type
		if pointer, ok := parameterType.(*ast.StarExpr); ok {
			parameterType = pointer.X
		}
		var typeName string
		switch typeExpr := parameterType.(type) {
		case *ast.Ident:
			typeName = typeExpr.Name
		case *ast.SelectorExpr:
			typeName = typeExpr.Sel.Name
		default:
			continue
		}
		for _, name := range nameList(field.Names) {
			for _, scopeField := range []string{"TeamID", "WorkspaceID"} {
				if workspaceTypes[typeName][scopeField] {
					return workspaceParameter{name: name, fieldName: scopeField}, true
				}
			}
		}
	}
	// Request-based workflows commonly unpack TeamID into a local variable
	// before validating it. Treat that alias like a direct workspace parameter.
	if function.Body != nil {
		for _, statement := range function.Body.List {
			assignment, ok := statement.(*ast.AssignStmt)
			if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
				continue
			}
			left, leftOK := assignment.Lhs[0].(*ast.Ident)
			right, rightOK := assignment.Rhs[0].(*ast.SelectorExpr)
			base, baseOK := (*ast.Ident)(nil), false
			if rightOK {
				base, baseOK = right.X.(*ast.Ident)
			}
			if leftOK && rightOK && baseOK && (right.Sel.Name == "TeamID" || right.Sel.Name == "WorkspaceID") {
				for _, field := range function.Type.Params.List {
					for _, name := range field.Names {
						if name.Name == base.Name {
							return workspaceParameter{name: left.Name}, true
						}
					}
				}
			}
		}
	}
	return workspaceParameter{}, false
}

func nameList(names []*ast.Ident) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name.Name)
	}
	return result
}

func workspaceStructFields(file *ast.File) map[string]map[string]bool {
	fields := make(map[string]map[string]bool)
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range structure.Fields.List {
				for _, name := range field.Names {
					if name.Name == "TeamID" || name.Name == "WorkspaceID" {
						if fields[typeSpec.Name.Name] == nil {
							fields[typeSpec.Name.Name] = make(map[string]bool)
						}
						fields[typeSpec.Name.Name][name.Name] = true
					}
				}
			}
		}
	}
	return fields
}

func contractWorkspaceStructFields(fset *token.FileSet) (map[string]map[string]bool, error) {
	fields := make(map[string]map[string]bool)
	for _, pattern := range []string{"internal/model/*.go", "internal/appmodel/*.go"} {
		sources, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("list application contract sources: %w", err)
		}
		for _, source := range sources {
			if strings.HasSuffix(source, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, source, nil, 0)
			if err != nil {
				return nil, fmt.Errorf("parse application contract %s: %w", source, err)
			}
			fields = mergeWorkspaceStructFields(fields, workspaceStructFields(file))
		}
	}
	return fields, nil
}

func mergeWorkspaceStructFields(left, right map[string]map[string]bool) map[string]map[string]bool {
	if left == nil {
		left = make(map[string]map[string]bool)
	}
	for typeName, fields := range right {
		if left[typeName] == nil {
			left[typeName] = make(map[string]bool)
		}
		for name := range fields {
			left[typeName][name] = true
		}
	}
	return left
}

func checkMethod(fset *token.FileSet, source, method string, file *ast.File, contractFields map[string]map[string]bool) error {
	workspaceTypes := mergeWorkspaceStructFields(workspaceStructFields(file), contractFields)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != method {
			continue
		}
		workspace, scoped := workspaceParameterFor(function, workspaceTypes)
		if !scoped {
			continue
		}
		var guard token.Pos
		for _, statement := range function.Body.List {
			if guardRejectsMissingWorkspace(statement, workspace) {
				guard = statement.Pos()
				break
			}
		}
		if !guard.IsValid() {
			return fmt.Errorf("%s:%d %s must return an error when its workspace ID is non-positive", source, fset.Position(function.Pos()).Line, method)
		}
		dependency := firstPortCall(function)
		if dependency.IsValid() && guard > dependency {
			return fmt.Errorf("%s:%d %s checks its workspace ID after calling a dependency", source, fset.Position(dependency).Line, method)
		}
		return nil
	}
	return fmt.Errorf("%s: workspace-facing method %s was not found", source, method)
}

func firstPortCall(function *ast.FuncDecl) token.Pos {
	if function.Recv == nil || len(function.Recv.List) == 0 || len(function.Recv.List[0].Names) == 0 {
		return token.NoPos
	}
	receiver := function.Recv.List[0].Names[0].Name
	first := token.NoPos
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		port, ok := method.X.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		root, ok := port.X.(*ast.Ident)
		if !ok || root.Name != receiver {
			return true
		}
		if !first.IsValid() || call.Pos() < first {
			first = call.Pos()
		}
		return true
	})
	return first
}

func guardRejectsMissingWorkspace(statement ast.Stmt, workspace workspaceParameter) bool {
	conditional, ok := statement.(*ast.IfStmt)
	if !ok || !branchReturns(conditional.Body) || !conditionRejectsMissingWorkspace(conditional.Cond, workspace) {
		return false
	}
	return true
}

func branchReturns(body *ast.BlockStmt) bool {
	for _, statement := range body.List {
		returned, ok := statement.(*ast.ReturnStmt)
		if !ok || len(returned.Results) == 0 {
			continue
		}
		last := returned.Results[len(returned.Results)-1]
		if value, ok := last.(*ast.Ident); !ok || value.Name != "nil" {
			return true
		}
	}
	return false
}

func conditionRejectsMissingWorkspace(expression ast.Expr, workspace workspaceParameter) bool {
	// A disjunction is safe when one branch is sufficient to reject a missing
	// workspace. A conjunction is deliberately not accepted: it can make the
	// missing-workspace check conditional on an unrelated predicate.
	if logical, ok := expression.(*ast.BinaryExpr); ok && logical.Op == token.LOR {
		return conditionRejectsMissingWorkspace(logical.X, workspace) || conditionRejectsMissingWorkspace(logical.Y, workspace)
	}
	comparison, ok := expression.(*ast.BinaryExpr)
	if !ok {
		return false
	}
	if !isWorkspaceID(comparison.X, workspace) {
		return false
	}
	return numericGuard(comparison.Op, comparison.Y)
}

func isWorkspaceID(expression ast.Expr, workspace workspaceParameter) bool {
	switch value := expression.(type) {
	case *ast.Ident:
		return workspace.fieldName == "" && value.Name == workspace.name
	case *ast.SelectorExpr:
		base, ok := value.X.(*ast.Ident)
		return ok && base.Name == workspace.name && value.Sel.Name == workspace.fieldName
	default:
		return false
	}
}

func numericGuard(operator token.Token, expression ast.Expr) bool {
	value, ok := integer(expression)
	if !ok {
		return false
	}
	switch operator {
	case token.LEQ:
		return value == 0
	case token.LSS:
		return value == 1
	default:
		return false
	}
}

func integer(expression ast.Expr) (int64, bool) {
	constant, ok := expression.(*ast.BasicLit)
	if !ok || constant.Kind != token.INT {
		return 0, false
	}
	value, err := strconv.ParseInt(constant.Value, 0, 64)
	return value, err == nil
}
