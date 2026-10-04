package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

func checkDatabaseMutationCommandShape(fset *token.FileSet) error {
	paths, err := filepath.Glob("internal/db/*.go")
	if err != nil {
		return fmt.Errorf("find database source files: %w", err)
	}
	var functions []*ast.FuncDecl
	functionsByName := make(map[string][]*ast.FuncDecl)
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			functions = append(functions, function)
			functionsByName[function.Name.Name] = append(functionsByName[function.Name.Name], function)
		}
	}
	mutatingPrefixes := []string{
		"Accept", "Assign", "Attach", "Audit", "Claim", "Complete", "Consume",
		"Create", "Delete", "Detach", "Enqueue", "Focus", "GetOrCreate",
		"Import", "Log", "Mark", "Pause", "Purge", "Remove", "Rename",
		"Reopen", "Resume", "Retry", "Set", "Start", "Stop", "Sync",
		"Touch", "Transfer", "Update", "Upsert",
	}
	for _, function := range functions {
		if function.Recv == nil || !hasDBReceiver(function) {
			continue
		}
		mutating := hasMutatingPersistenceCall(function, functionsByName, make(map[*ast.FuncDecl]bool))
		for _, prefix := range mutatingPrefixes {
			if strings.HasPrefix(function.Name.Name, prefix) {
				mutating = true
				break
			}
		}
		if !function.Name.IsExported() || !mutating {
			continue
		}
		path := fset.Position(function.Pos()).Filename
		if err := validateMutationSignature(path, function); err != nil {
			return err
		}
	}
	return nil
}

func validateMutationSignature(path string, function *ast.FuncDecl) error {
	var parameters []*ast.Field
	if function.Type.Params != nil {
		parameters = function.Type.Params.List
	}
	if len(parameters) == 0 || !isQualifiedType(parameters[0].Type, "context", "Context") {
		return fmt.Errorf("%s %s must accept context.Context as its first parameter", path, function.Name.Name)
	}
	if len(parameters) > 2 || hasMultipleNames(parameters) || len(parameters) == 2 && !isCommandValueType(parameters[1].Type) {
		return fmt.Errorf("%s %s must accept context.Context and at most one typed command", path, function.Name.Name)
	}
	return nil
}

func hasMutatingPersistenceCall(function *ast.FuncDecl, functionsByName map[string][]*ast.FuncDecl, visited map[*ast.FuncDecl]bool) bool {
	if function == nil || function.Body == nil || visited[function] {
		return false
	}
	visited[function] = true
	found := false
	var calledNames []string
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch callee := call.Fun.(type) {
		case *ast.Ident:
			name = callee.Name
		case *ast.SelectorExpr:
			name = callee.Sel.Name
		}
		switch name {
		case "BeginTx", "Exec", "ExecContext", "Commit":
			found = true
			return false
		}
		if name != "" {
			calledNames = append(calledNames, name)
		}
		return true
	})
	if found {
		return true
	}
	for _, name := range calledNames {
		for _, callee := range functionsByName[name] {
			if hasMutatingPersistenceCall(callee, functionsByName, visited) {
				return true
			}
		}
	}
	return false
}

func isCommandValueType(expression ast.Expr) bool {
	if pointer, ok := expression.(*ast.StarExpr); ok {
		return isCommandValueType(pointer.X)
	}
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	packageName, ok := selector.X.(*ast.Ident)
	if !ok {
		return false
	}
	typeName := selector.Sel.Name
	switch packageName.Name {
	case "mailport":
		return typeName == "InvoiceEmailJob" || typeName == "InvoiceEmailRetryRequest"
	case "appmodel":
		return strings.HasSuffix(typeName, "Request") || strings.HasSuffix(typeName, "Command")
	case "webhookport":
		switch typeName {
		case "DeliveryJob", "CommittedEvent", "DeliveryRetryRequest", "EventRetryRequest":
			return true
		}
		return false
	case "model":
		if strings.HasSuffix(typeName, "Record") {
			return true
		}
		return false
	default:
		return false
	}
}

func checkTypedPersistenceCommands(fset *token.FileSet, parsed map[string]*ast.File) error {
	commands := []struct {
		source, method, request string
	}{
		{"internal/db/auth.go", "CreateAccount", "AccountCreateRequest"},
		{"internal/db/activities.go", "GetOrCreateActivityForMember", "ActivityResolveRequest"},
		{"internal/db/team_invites.go", "CreateTeamInvite", "TeamInvitePersistenceRequest"},
		{"internal/db/projects.go", "CreateProjectWithBilling", "ProjectCreateRequest"},
		{"internal/db/projects.go", "UpdateProjectWithOptions", "ProjectUpdateRequest"},
		{"internal/db/auth.go", "UpdateUserName", "ProfileNameRequest"},
		{"internal/db/auth.go", "UpdateUserPasswordIfHashMatches", "PasswordHashUpdateRequest"},
		{"internal/db/auth.go", "CreateAuthSession", "AuthSessionCreateRequest"},
		{"internal/db/tokens.go", "APITokenByRaw", "APITokenLookupRequest"},
		{"internal/db/auth.go", "DeleteAuthSession", "AuthSessionDeleteRequest"},
		{"internal/db/auth.go", "DeleteAuthSessionsByUser", "AuthSessionsDeleteByUserRequest"},
		{"internal/db/auth.go", "TouchAuthSession", "AuthSessionTouchRequest"},
		{"internal/db/auth.go", "PurgeExpiredAuthSessions", "AuthSessionsPurgeExpiredRequest"},
		{"internal/db/auth.go", "CreatePasswordReset", "PasswordResetCreateRequest"},
		{"internal/db/auth.go", "ConsumePasswordReset", "PasswordResetConsumeRequest"},
		{"internal/db/invoice_transitions.go", "DeleteInvoice", "InvoiceMutationRequest"},
		{"internal/db/invoices.go", "CreateInvoice", "InvoiceCreateRequest"},
		{"internal/db/invoice_transitions.go", "RebuildInvoice", "InvoiceMutationRequest"},
		{"internal/db/invoice_transitions.go", "MarkInvoiceSentOnce", "InvoiceMutationRequest"},
		{"internal/db/invoice_payments.go", "SetPaymentURL", "InvoicePaymentLinkSaveRequest"},
		{"internal/db/invoice_payments.go", "MarkInvoicePaidOnce", "InvoiceMutationRequest"},
		{"internal/db/invoice_payments.go", "MarkInvoicePaidFromStripe", "InvoiceStripePaymentRequest"},
		{"internal/db/invoice_payments.go", "SetInvoiceReceipt", "InvoiceReceiptRequest"},
		{"internal/db/project_assignments.go", "AssignUnassignedActivityForBilling", "AssignActivityProjectRequest"},
		{"internal/db/goals.go", "UpsertGoalForManager", "GoalUpsertRequest"},
		{"internal/db/goals.go", "DeleteGoalForManager", "GoalDeleteRequest"},
		{"internal/db/webhooks.go", "CreateWebhook", "WebhookRegistrationCommand"},
		{"internal/db/webhooks.go", "LogWebhookDelivery", "WebhookDeliveryLogRequest"},
		{"internal/db/mailqueue.go", "RetryInvoiceEmail", "mailport.InvoiceEmailRetryRequest"},
		{"internal/db/webhook_queue.go", "RetryWebhookDelivery", "webhookport.DeliveryRetryRequest"},
		{"internal/db/webhook_events.go", "RetryWebhookEvent", "webhookport.EventRetryRequest"},
		{"internal/db/webhook_queue.go", "EnqueueWebhookDeliveries", "WebhookDeliveryBatchRequest"},
		{"internal/db/preferences.go", "SetUserPrefs", "UserPrefsSaveCommand"},
		{"internal/db/integration_sync.go", "SyncExternalTasks", "IntegrationTaskSyncRequest"},
		{"internal/db/imports.go", "ImportEntries", "ImportBatchRequest"},
		{"internal/db/payroll_creation.go", "CreatePayrollDraft", "PayrollDraftRequest"},
		{"internal/db/payroll_transitions.go", "MarkPayrollPaidWithRecipients", "PayrollMutationRequest"},
		{"internal/db/payroll_transitions.go", "DeletePayrollDraft", "PayrollMutationRequest"},
		{"internal/db/payroll_members.go", "SetMemberPay", "PayrollMemberPayRequest"},
		{"internal/db/sessions.go", "StartSession", "TimerStartRequest"},
		{"internal/db/timesheet.go", "UpsertDayTotal", "TimesheetCellUpdateRequest"},
		{"internal/db/sessions.go", "CreateClosedSession", "TimerAddRequest"},
		{"internal/db/tracking.go", "FocusActivity", "TimerFocusRequest"},
		{"internal/db/tracking.go", "StopActiveSessions", "TimerStopAllRequest"},
		{"internal/db/tracking.go", "PauseActiveSessions", "TimerStopAllRequest"},
		{"internal/db/session_transitions.go", "UpdateSessionEnd", "TimerStopRequest"},
		{"internal/db/session_transitions.go", "PauseSession", "TimerSessionRequest"},
		{"internal/db/session_transitions.go", "ResumeSession", "TimerSessionRequest"},
		{"internal/db/session_transitions.go", "ReopenSession", "TimerReopenRequest"},
	}
	for _, command := range commands {
		file := parsed[command.source]
		if file == nil {
			parsedFile, err := parser.ParseFile(fset, command.source, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", command.source, err)
			}
			file = parsedFile
			parsed[command.source] = file
		}
		if !hasTypedCommandParameter(file, command.method, command.request) {
			return fmt.Errorf("%s %s must accept context.Context and %s", command.source, command.method, command.request)
		}
	}
	return nil
}

func hasTypedCommandParameter(file *ast.File, method, request string) bool {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != method || !hasDBReceiver(function) || function.Type.Params == nil {
			continue
		}
		parameters := function.Type.Params.List
		if len(parameters) != 2 || hasMultipleNames(parameters) || !isQualifiedType(parameters[0].Type, "context", "Context") {
			return false
		}
		parts := strings.SplitN(request, ".", 2)
		if len(parts) == 2 {
			return isQualifiedType(parameters[1].Type, parts[0], parts[1])
		}
		return isQualifiedType(parameters[1].Type, "appmodel", request)
	}
	return false
}

func hasDBReceiver(function *ast.FuncDecl) bool {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	receiver := function.Recv.List[0].Type
	pointer, ok := receiver.(*ast.StarExpr)
	if !ok {
		return false
	}
	identifier, ok := pointer.X.(*ast.Ident)
	return ok && identifier.Name == "DB"
}

func hasMultipleNames(parameters []*ast.Field) bool {
	for _, parameter := range parameters {
		if len(parameter.Names) > 1 {
			return true
		}
	}
	return false
}

func isQualifiedType(expression ast.Expr, packageName, typeName string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != typeName {
		return false
	}
	identifier, ok := selector.X.(*ast.Ident)
	return ok && identifier.Name == packageName
}
