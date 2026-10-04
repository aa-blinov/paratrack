package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestMutationDetectionFollowsPrivatePersistenceHelpers(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", `package db
func (d *DB) Rename(ctx context.Context, request appmodel.RenameRequest) error {
	return d.persistRename(ctx)
}

func (d *DB) persistRename(ctx context.Context) error {
	_, err := d.sql.ExecContext(ctx, "UPDATE teams SET name = ?")
	return err
}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := make(map[string][]*ast.FuncDecl)
	var rename *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		functions[function.Name.Name] = append(functions[function.Name.Name], function)
		if function.Name.Name == "Rename" {
			rename = function
		}
	}
	if !hasMutatingPersistenceCall(rename, functions, make(map[*ast.FuncDecl]bool)) {
		t.Fatal("Rename was not classified as mutating through persistRename")
	}
}

func TestMutationDetectionDistinguishesReadOnlyTransactions(t *testing.T) {
	cases := []struct {
		name      string
		options   string
		wantWrite bool
	}{
		{name: "read only", options: "&sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}"},
		{name: "read write", options: "&sql.TxOptions{Isolation: sql.LevelRepeatableRead}", wantWrite: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fset := token.NewFileSet()
			source := "package db\nfunc (d *DB) Read(ctx context.Context) error { tx, err := d.sql.BeginTx(ctx, " + testCase.options + "); if err != nil { return err }; rows, err := tx.QueryContext(ctx, \"SELECT 1\"); if err != nil { return err }; defer rows.Close(); return tx.Commit() }"
			file, err := parser.ParseFile(fset, "fixture.go", source, 0)
			if err != nil {
				t.Fatal(err)
			}
			function := file.Decls[0].(*ast.FuncDecl)
			gotWrite := hasMutatingPersistenceCall(function, map[string][]*ast.FuncDecl{}, make(map[*ast.FuncDecl]bool))
			if gotWrite != testCase.wantWrite {
				t.Fatalf("read transaction classified as write = %v, want %v", gotWrite, testCase.wantWrite)
			}
		})
	}
}

func TestMutationSignatureRequiresContextAndTypedCommand(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		wantErr string
	}{
		{
			name:    "missing context",
			source:  "func (d *DB) Write(id int64) error { return nil }",
			wantErr: "must accept context.Context as its first parameter",
		},
		{
			name:    "positional mutation input",
			source:  "func (d *DB) Write(ctx context.Context, id int64) error { return nil }",
			wantErr: "at most one typed command",
		},
		{
			name:   "typed request",
			source: "func (d *DB) Write(ctx context.Context, request appmodel.WriteRequest) error { return nil }",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package db\n"+testCase.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			function := file.Decls[0].(*ast.FuncDecl)
			err = validateMutationSignature("fixture.go", function)
			if testCase.wantErr == "" {
				if err != nil {
					t.Fatalf("validateMutationSignature() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("validateMutationSignature() error = %v, want text %q", err, testCase.wantErr)
			}
		})
	}
}

func TestTransactionalSnapshotMustPrecedeCommit(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "snapshot before commit",
			body: `func (d *DB) Create(ctx context.Context) error {
				writeSomething()
				_, err := readSnapshot(ctx)
				if err != nil { return err }
				return tx.Commit()
			}`,
		},
		{
			name: "snapshot after commit",
			body: `func (d *DB) Create(ctx context.Context) error {
				writeSomething()
				if err := tx.Commit(); err != nil { return err }
				_, err := readSnapshot(ctx)
				return err
			}`,
			wantErr: true,
		},
		{
			name: "snapshot before write",
			body: `func (d *DB) Create(ctx context.Context) error {
				_, err := readSnapshot(ctx)
				if err != nil { return err }
				writeSomething()
				return tx.Commit()
			}`,
			wantErr: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "fixture.go", "package db\n"+testCase.body, 0)
			if err != nil {
				t.Fatal(err)
			}
			err = validateTransactionalSnapshot(fset, "fixture.go", file, "Create", "readSnapshot", "writeSomething", 1)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("validateTransactionalSnapshot() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}

func TestInvoiceNumberAllocationUsesTheCurrentTransaction(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "same transaction",
			body: `func (d *DB) CreateInvoiceDraft(ctx context.Context) error {
				_, err := nextInvoiceNumberWithRules(ctx, tx, teamID, rules, now)
				return err
			}`,
		},
		{
			name: "checks out another connection",
			body: `func (d *DB) CreateInvoiceDraft(ctx context.Context) error {
				_, err := d.nextInvoiceNumber(ctx, teamID, now)
				return err
			}`,
			wantErr: true,
		},
		{
			name: "helper receives another queryer",
			body: `func (d *DB) CreateInvoiceDraft(ctx context.Context) error {
				_, err := nextInvoiceNumberWithRules(ctx, d.sql, teamID, rules, now)
				return err
			}`,
			wantErr: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package db\n"+testCase.body, 0)
			if err != nil {
				t.Fatal(err)
			}
			err = validateInvoiceNumberUsesTransaction("fixture.go", file, "CreateInvoiceDraft")
			if (err != nil) != testCase.wantErr {
				t.Fatalf("validateInvoiceNumberUsesTransaction() error = %v, wantErr %v", err, testCase.wantErr)
			}
		})
	}
}
