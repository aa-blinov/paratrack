package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

func checkDatabaseContextCalls(fset *token.FileSet, parsed map[string]*ast.File) error {
	sources, err := filepath.Glob("internal/db/*.go")
	if err != nil {
		return fmt.Errorf("list database sources: %w", err)
	}
	contextless := map[string]bool{
		"Begin": true, "Exec": true, "Ping": true, "Prepare": true,
		"Query": true, "QueryRow": true,
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
		var violation token.Pos
		var method string
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && contextless[selector.Sel.Name] {
				violation, method = call.Pos(), selector.Sel.Name
				return false
			}
			return true
		})
		if violation.IsValid() {
			return fmt.Errorf("%s:%d uses contextless database-style method %s; use its context-aware variant", source, fset.Position(violation).Line, method)
		}
	}
	return nil
}

func checkTransactionalEvents(fset *token.FileSet, parsed map[string]*ast.File) error {
	producers := []struct{ source, method string }{
		{"internal/db/sessions.go", "StartSession"},
		{"internal/db/sessions.go", "UpdateSessionFields"},
		{"internal/db/tracking.go", "FocusActivity"},
		{"internal/db/tracking.go", "StopActiveSessions"},
		{"internal/db/tracking.go", "stopMemberSessionsTx"},
		{"internal/db/session_transitions.go", "UpdateSessionEnd"},
		{"internal/db/invoices.go", "CreateInvoiceDraft"},
		{"internal/db/invoice_payments.go", "SetPaymentURL"},
		{"internal/db/invoice_payments.go", "MarkInvoicePaidOnce"},
		{"internal/db/invoice_payments.go", "MarkInvoicePaidFromStripe"},
		{"internal/db/imports.go", "importEntries"},
	}
	declared := make(map[string]bool, len(producers))
	for _, producer := range producers {
		declared[producer.source+":"+producer.method] = true
		file := parsed[producer.source]
		if file == nil {
			parsedFile, err := parser.ParseFile(fset, producer.source, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", producer.source, err)
			}
			file = parsedFile
			parsed[producer.source] = file
		}
		if !methodRecordsTransactionalEvent(file, producer.method) {
			return fmt.Errorf("%s %s must record its outbound event with the business transaction", producer.source, producer.method)
		}
	}
	if err := discoverTransactionalEvents(fset, parsed, declared); err != nil {
		return err
	}
	return nil
}

func discoverTransactionalEvents(fset *token.FileSet, parsed map[string]*ast.File, declared map[string]bool) error {
	sources, err := filepath.Glob("internal/db/*.go")
	if err != nil {
		return fmt.Errorf("list database sources: %w", err)
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
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || !functionCallsTransactionalEvent(function) {
				continue
			}
			key := source + ":" + function.Name.Name
			if !declared[key] {
				return fmt.Errorf("%s %s emits an outbox event but is not listed as a transactional producer", source, function.Name.Name)
			}
		}
	}
	return nil
}

func functionCallsTransactionalEvent(function *ast.FuncDecl) bool {
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		found = ok && method.Sel.Name == "recordWebhookEventTx"
		return !found
	})
	return found
}

func methodRecordsTransactionalEvent(file *ast.File, name string) bool {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != name || function.Body == nil {
			continue
		}
		found := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			conditional, ok := node.(*ast.IfStmt)
			if !ok {
				return true
			}
			assignment, ok := conditional.Init.(*ast.AssignStmt)
			if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 {
				return true
			}
			call, ok := assignment.Rhs[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || method.Sel.Name != "recordWebhookEventTx" || len(call.Args) < 2 {
				return true
			}
			transaction, ok := call.Args[1].(*ast.Ident)
			if !ok || transaction.Name != "tx" {
				return true
			}
			errVariable, ok := assignment.Lhs[0].(*ast.Ident)
			if !ok || !conditionChecksError(conditional.Cond, errVariable.Name) || !branchReturnsError(conditional.Body, errVariable.Name) {
				return true
			}
			found = true
			return !found
		})
		return found
	}
	return false
}

func conditionChecksError(expression ast.Expr, variable string) bool {
	comparison, ok := expression.(*ast.BinaryExpr)
	if !ok || comparison.Op != token.NEQ {
		return false
	}
	left, leftOK := comparison.X.(*ast.Ident)
	right, rightOK := comparison.Y.(*ast.Ident)
	return (leftOK && left.Name == variable && rightOK && right.Name == "nil") ||
		(rightOK && right.Name == variable && leftOK && left.Name == "nil")
}

func branchReturnsError(body *ast.BlockStmt, variable string) bool {
	for _, statement := range body.List {
		returned, ok := statement.(*ast.ReturnStmt)
		if !ok || len(returned.Results) == 0 {
			continue
		}
		last := returned.Results[len(returned.Results)-1]
		if expressionReferencesIdentifier(last, variable) {
			return true
		}
	}
	return false
}

func expressionReferencesIdentifier(expression ast.Expr, name string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && ident.Name == name {
			found = true
			return false
		}
		return !found
	})
	return found
}

func checkDatabaseTransactions(fset *token.FileSet, parsed map[string]*ast.File) error {
	sources, err := filepath.Glob("internal/db/*.go")
	if err != nil {
		return fmt.Errorf("list database sources: %w", err)
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") || source == "internal/db/conn.go" {
			continue
		}
		file := parsed[source]
		if file == nil {
			file, err = parser.ParseFile(fset, source, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", source, err)
			}
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			begins, rollbacks := transactionVariables(function)
			for variable, positions := range begins {
				deferred := rollbacks[variable]
				if len(deferred) < len(positions) {
					position := positions[len(deferred)]
					return fmt.Errorf("%s:%d %s must defer rollback for transaction %s", source, fset.Position(position).Line, function.Name.Name, variable)
				}
				for index, position := range positions {
					if deferred[index] <= position {
						return fmt.Errorf("%s:%d %s must defer rollback after beginning transaction %s", source, fset.Position(position).Line, function.Name.Name, variable)
					}
				}
			}
		}
	}
	return nil
}

func checkTransactionalSnapshots(fset *token.FileSet, parsed map[string]*ast.File) error {
	checks := []struct {
		source, method, reader, after string
		minimum                       int
	}{
		{"internal/db/sessions.go", "createLegacySession", "getSessionTx", "QueryRowContext", 1},
		{"internal/db/sessions.go", "StartSession", "getSessionTx", "recordWebhookEventTx", 1},
		{"internal/db/sessions.go", "CreateClosedSession", "getSessionTx", "insertClosedSessionTx", 1},
		{"internal/db/session_transitions.go", "UpdateSessionEnd", "getSessionTx", "recordWebhookEventTx", 2},
		{"internal/db/session_transitions.go", "PauseSession", "getSessionTx", "ExecContext", 2},
		{"internal/db/session_transitions.go", "ResumeSession", "getSessionTx", "ExecContext", 2},
		{"internal/db/session_transitions.go", "ReopenSession", "getSessionTx", "ExecContext", 2},
		{"internal/db/invoices.go", "CreateInvoice", "getInvoice", "insertLines", 1},
		{"internal/db/invoices.go", "CreateInvoiceDraft", "getInvoice", "recordWebhookEventTx", 1},
		{"internal/db/invoices.go", "CreateInvoiceDraft", "listInvoiceLines", "recordWebhookEventTx", 1},
		{"internal/db/payroll_creation.go", "createPayrollRun", "getPayrollRun", "insertPayrollRunTx", 1},
		{"internal/db/payroll_creation.go", "CreatePayrollDraft", "getPayrollRun", "insertPayrollRunTx", 1},
		{"internal/db/saved_reports.go", "CreateSavedReport", "getSavedReport", "QueryRowContext", 1},
	}
	for _, check := range checks {
		file := parsed[check.source]
		if file == nil {
			var err error
			file, err = parser.ParseFile(fset, check.source, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", check.source, err)
			}
			parsed[check.source] = file
		}
		if err := validateTransactionalSnapshot(fset, check.source, file, check.method, check.reader, check.after, check.minimum); err != nil {
			return err
		}
	}
	return nil
}

func checkInvoiceNumberUsesTransaction(fset *token.FileSet, parsed map[string]*ast.File) error {
	const source, method = "internal/db/invoices.go", "CreateInvoiceDraft"
	file := parsed[source]
	if file == nil {
		var err error
		file, err = parser.ParseFile(fset, source, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", source, err)
		}
		parsed[source] = file
	}
	return validateInvoiceNumberUsesTransaction(source, file, method)
}

func validateInvoiceNumberUsesTransaction(source string, file *ast.File, method string) error {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != method || function.Body == nil {
			continue
		}
		found := false
		invalid := false
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch callee := call.Fun.(type) {
			case *ast.Ident:
				name = callee.Name
			case *ast.SelectorExpr:
				name = callee.Sel.Name
			}
			if name == "nextInvoiceNumber" {
				invalid = true
				return false
			}
			if name == "nextInvoiceNumberWithRules" {
				if len(call.Args) < 2 {
					invalid = true
					return false
				}
				queryer, ok := call.Args[1].(*ast.Ident)
				found = ok && queryer.Name == "tx"
				invalid = !found
				return false
			}
			return true
		})
		if invalid || !found {
			return fmt.Errorf("%s %s must allocate the invoice number through its transaction", source, method)
		}
		return nil
	}
	return fmt.Errorf("%s %s not found for transaction pool check", source, method)
}

func validateTransactionalSnapshot(fset *token.FileSet, source string, file *ast.File, method, reader, after string, minimum int) error {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != method || function.Body == nil {
			continue
		}
		var lastCommit token.Pos
		var lastRead token.Pos
		var lastWrite token.Pos
		readCount := 0
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch callee := call.Fun.(type) {
			case *ast.Ident:
				name = callee.Name
			case *ast.SelectorExpr:
				name = callee.Sel.Name
			}
			if name == "Commit" {
				lastCommit = call.Pos()
			}
			if name == after {
				lastWrite = call.Pos()
			}
			if name == reader {
				readCount++
				lastRead = call.Pos()
			}
			return true
		})
		if readCount < minimum || !lastWrite.IsValid() || !lastCommit.IsValid() || !lastRead.IsValid() || lastRead <= lastWrite || lastRead >= lastCommit {
			return fmt.Errorf("%s %s must read its returned %s snapshot after the write and before commit", source, method, reader)
		}
		return nil
	}
	return fmt.Errorf("%s %s not found for transactional snapshot check", source, method)
}

func checkDatabaseRowsClosed(fset *token.FileSet, parsed map[string]*ast.File) error {
	sources, err := filepath.Glob("internal/db/*.go")
	if err != nil {
		return fmt.Errorf("list database sources: %w", err)
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") || source == "internal/db/conn.go" {
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
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			queries := queryRowsVariables(function)
			for index, query := range queries {
				var nextQuery token.Pos
				for _, next := range queries[index+1:] {
					if next.variable == query.variable {
						nextQuery = next.position
						break
					}
				}
				if err := checkRowsClosedOnReturns(function, query, nextQuery); err != nil {
					return fmt.Errorf("%s:%d %s: %w", source, fset.Position(query.position).Line, function.Name.Name, err)
				}
			}
			iterations := rowIterationVariables(function)
			for index, iteration := range iterations {
				var nextIteration token.Pos
				for _, next := range iterations[index+1:] {
					if next.variable == iteration.variable {
						nextIteration = next.start
						break
					}
				}
				if !rowsErrorsCheckedAfter(function, iteration.variable, iteration.end, nextIteration) {
					return fmt.Errorf("%s:%d %s must check rows.Err() after iterating %s", source, fset.Position(iteration.start).Line, function.Name.Name, iteration.variable)
				}
			}
		}
	}
	return nil
}

type rowIteration struct {
	variable string
	start    token.Pos
	end      token.Pos
}

func rowIterationVariables(function *ast.FuncDecl) []rowIteration {
	var iterations []rowIteration
	ast.Inspect(function.Body, func(node ast.Node) bool {
		loop, ok := node.(*ast.ForStmt)
		if !ok || loop.Cond == nil {
			return true
		}
		call, ok := loop.Cond.(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != "Next" {
			return true
		}
		variable, ok := method.X.(*ast.Ident)
		if ok && variable.Name != "_" {
			iterations = append(iterations, rowIteration{variable: variable.Name, start: loop.Pos(), end: loop.End()})
		}
		return true
	})
	return iterations
}

func rowsErrorsCheckedAfter(function *ast.FuncDecl, variable string, loopEnd, nextIteration token.Pos) bool {
	found := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || call.Pos() <= loopEnd || (nextIteration.IsValid() && call.Pos() >= nextIteration) {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != "Err" {
			return true
		}
		rows, ok := method.X.(*ast.Ident)
		if ok && rows.Name == variable {
			found = true
		}
		return !found
	})
	return found
}

type rowQuery struct {
	variable string
	position token.Pos
}

func queryRowsVariables(function *ast.FuncDecl) []rowQuery {
	var queries []rowQuery
	ast.Inspect(function.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || !containsQueryContext(assignment.Rhs) {
			return true
		}
		for _, left := range assignment.Lhs {
			if variable, ok := left.(*ast.Ident); ok && variable.Name != "_" {
				queries = append(queries, rowQuery{variable: variable.Name, position: assignment.Pos()})
				break
			}
		}
		return true
	})
	return queries
}

func checkRowsClosedOnReturns(function *ast.FuncDecl, query rowQuery, nextQuery token.Pos) error {
	var closePositions, returnPositions []token.Pos
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.DeferStmt:
			if isRowsClose(value.Call, query.variable) {
				closePositions = append(closePositions, value.Pos())
			}
		case *ast.CallExpr:
			if isRowsClose(value, query.variable) {
				closePositions = append(closePositions, value.Pos())
			}
		case *ast.ReturnStmt:
			returnPositions = append(returnPositions, value.Pos())
		}
		return true
	})
	closeBetween := func(start, end token.Pos) bool {
		for _, position := range closePositions {
			if position > start && (!end.IsValid() || position < end) {
				return true
			}
		}
		return false
	}
	if !closeBetween(query.position, nextQuery) {
		return fmt.Errorf("query rows %s have no Close() after the query", query.variable)
	}
	firstClose := token.Pos(0)
	for _, position := range closePositions {
		if position > query.position && (!nextQuery.IsValid() || position < nextQuery) && (firstClose == 0 || position < firstClose) {
			firstClose = position
		}
	}
	for _, position := range returnPositions {
		if position <= query.position || (nextQuery.IsValid() && position >= nextQuery) {
			continue
		}
		// The standard QueryContext error guard returns before the first Close
		// because no rows resource was acquired on that path.
		if position < firstClose {
			continue
		}
		if !closeBetween(query.position, position) {
			return fmt.Errorf("query rows %s may remain open on an early return", query.variable)
		}
	}
	return nil
}

func isRowsClose(call *ast.CallExpr, variable string) bool {
	method, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || method.Sel.Name != "Close" {
		return false
	}
	rows, ok := method.X.(*ast.Ident)
	return ok && rows.Name == variable
}

func containsQueryContext(expressions []ast.Expr) bool {
	found := false
	for _, expression := range expressions {
		ast.Inspect(expression, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			found = ok && method.Sel.Name == "QueryContext"
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

func transactionVariables(function *ast.FuncDecl) (map[string][]token.Pos, map[string][]token.Pos) {
	begins := make(map[string][]token.Pos)
	rollbacks := make(map[string][]token.Pos)
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.AssignStmt:
			if !containsBeginTx(value.Rhs) {
				return true
			}
			for _, left := range value.Lhs {
				if variable, ok := left.(*ast.Ident); ok && variable.Name != "_" {
					begins[variable.Name] = append(begins[variable.Name], value.Pos())
					break
				}
			}
		case *ast.DeferStmt:
			ast.Inspect(value.Call, func(child ast.Node) bool {
				call, ok := child.(*ast.CallExpr)
				if !ok {
					return true
				}
				method, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || method.Sel.Name != "Rollback" {
					return true
				}
				variable, ok := method.X.(*ast.Ident)
				if ok {
					rollbacks[variable.Name] = append(rollbacks[variable.Name], value.Pos())
				}
				return true
			})
		}
		return true
	})
	return begins, rollbacks
}

func containsBeginTx(expressions []ast.Expr) bool {
	found := false
	for _, expression := range expressions {
		ast.Inspect(expression, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			method, ok := call.Fun.(*ast.SelectorExpr)
			if ok && method.Sel.Name == "BeginTx" {
				found = true
				return false
			}
			return !found
		})
	}
	return found
}
