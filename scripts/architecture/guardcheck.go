// Command guardcheck verifies workspace-facing workflow scope and persistence invariants.
package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"strings"
)

func main() {
	if err := check(os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "architecture check:", err)
		os.Exit(1)
	}
}

func check(input io.Reader) error {
	fset := token.NewFileSet()
	contractFields, err := contractWorkspaceStructFields(fset)
	if err != nil {
		return err
	}
	files := make(map[string]*ast.File)
	var workflowPackages []string
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		entry := strings.TrimSpace(scanner.Text())
		if entry == "" {
			continue
		}
		if packagePath, ok := strings.CutPrefix(entry, "package:"); ok {
			workflowPackages = append(workflowPackages, packagePath)
			continue
		}
		source, method, ok := strings.Cut(entry, ":")
		if !ok || source == "" || method == "" {
			return fmt.Errorf("invalid workspace guard entry %q", entry)
		}
		file := files[source]
		if file == nil {
			parsed, err := parser.ParseFile(fset, source, nil, 0)
			if err != nil {
				return fmt.Errorf("parse %s: %w", source, err)
			}
			file = parsed
			files[source] = file
		}
		if err := checkMethod(fset, source, method, file, contractFields); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read workspace guard list: %w", err)
	}
	if err := checkWorkflowWorkspaceGuardCoverage(fset, files, workflowPackages, contractFields); err != nil {
		return err
	}
	if err := checkDatabaseTransactions(fset, files); err != nil {
		return err
	}
	if err := checkTransactionalSnapshots(fset, files); err != nil {
		return err
	}
	if err := checkInvoiceNumberUsesTransaction(fset, files); err != nil {
		return err
	}
	if err := checkDatabaseRowsClosed(fset, files); err != nil {
		return err
	}
	if err := checkDatabaseContextCalls(fset, files); err != nil {
		return err
	}
	if err := checkTypedPersistenceCommands(fset, files); err != nil {
		return err
	}
	if err := checkDatabaseMutationCommandShape(fset); err != nil {
		return err
	}
	if err := checkTransportGuards(fset); err != nil {
		return err
	}
	if err := checkTransactionalEvents(fset, files); err != nil {
		return err
	}
	return checkReportPersonScope(fset)
}
