package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

func checkTransportGuards(fset *token.FileSet) error {
	paths, err := filepath.Glob("internal/web/*.go")
	if err != nil {
		return fmt.Errorf("find HTTP adapter sources: %w", err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if err := validateParseFormErrorExposure(path, file); err != nil {
			return err
		}
		if err := validateJSONResponseDTOs(path, file); err != nil {
			return err
		}
	}
	return checkJSONSerialization(fset)
}

func checkJSONSerialization(fset *token.FileSet) error {
	return filepath.WalkDir("internal", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk internal source tree: %w", walkErr)
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		if err := validateJSONSerializationDTOs(path, file); err != nil {
			return err
		}
		return nil
	})
}

func validateJSONResponseDTOs(path string, file *ast.File) error {
	return validateJSONSerializationDTOs(path, file)
}

func validateJSONSerializationDTOs(path string, file *ast.File) error {
	var violation bool
	ast.Inspect(file, func(node ast.Node) bool {
		if assignment, ok := node.(*ast.AssignStmt); ok {
			for index, right := range assignment.Rhs {
				call, ok := right.(*ast.CallExpr)
				if !ok || !isJSONMarshalCall(call) {
					continue
				}
				// json.Marshal returns (bytes, error). The error is the
				// second result, regardless of whether the bytes are used.
				if len(assignment.Rhs) == 1 && index == 0 && len(assignment.Lhs) >= 2 {
					if identifier, ok := assignment.Lhs[1].(*ast.Ident); ok && identifier.Name == "_" {
						violation = true
					}
				}
			}
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		responseIndex := -1
		switch function := call.Fun.(type) {
		case *ast.Ident:
			switch function.Name {
			case "writeJSON":
				responseIndex = 1
			case "writeJSONStatus", "writeJSONContentType":
				responseIndex = 2
			}
		case *ast.SelectorExpr:
			switch function.Sel.Name {
			case "writeJSON":
				responseIndex = 1
			case "writeJSONStatus", "writeJSONContentType":
				responseIndex = 2
			case "Marshal":
				if receiver, ok := function.X.(*ast.Ident); ok && receiver.Name == "json" {
					responseIndex = 0
				}
			}
		}
		if responseIndex < 0 || len(call.Args) <= responseIndex {
			return true
		}
		literal, ok := call.Args[responseIndex].(*ast.CompositeLit)
		if !ok {
			return true
		}
		if _, ok := literal.Type.(*ast.MapType); ok {
			violation = true
		}
		return true
	})
	if violation {
		return fmt.Errorf("%s passes an inline map or ignores a JSON marshal error", path)
	}
	return nil
}

func isJSONMarshalCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Marshal" {
		return false
	}
	receiver, ok := selector.X.(*ast.Ident)
	return ok && receiver.Name == "json"
}

func validateParseFormErrorExposure(path string, file *ast.File) error {
	var violation bool
	ast.Inspect(file, func(node ast.Node) bool {
		statement, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		errorName, ok := parseFormErrorVariable(statement)
		if !ok {
			return true
		}
		ast.Inspect(statement.Body, func(bodyNode ast.Node) bool {
			call, ok := bodyNode.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Error" {
				return true
			}
			identifier, ok := selector.X.(*ast.Ident)
			if ok && identifier.Name == errorName {
				violation = true
			}
			return true
		})
		return true
	})
	if violation {
		return fmt.Errorf("%s exposes ParseForm's internal error; return a stable invalid-form response", path)
	}
	return nil
}

func parseFormErrorVariable(statement *ast.IfStmt) (string, bool) {
	if statement.Init == nil {
		return "", false
	}
	assignment, ok := statement.Init.(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
		return "", false
	}
	name, ok := assignment.Lhs[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	call, ok := assignment.Rhs[0].(*ast.CallExpr)
	if !ok {
		return "", false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return name.Name, ok && selector.Sel.Name == "ParseForm"
}
