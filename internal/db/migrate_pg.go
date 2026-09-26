package db

import (
	"context"
	"fmt"
	"strings"
)

// CopyToPostgres copies every table of the SQLite database src into the
// empty Postgres database dst in one transaction, keeping ids, then moves
// each identity sequence past the copied rows. It returns rows per table.
//
// Foreign keys are checked at commit (session_replication_role = replica
// skips them during the bulk load; needs a superuser, which the bundled
// Postgres container's user is). Row counts are compared before commit.
func CopyToPostgres(ctx context.Context, src, dst *DB) (map[string]int, error) {
	if src.IsPostgres() || !dst.IsPostgres() {
		return nil, fmt.Errorf("copy goes from SQLite to Postgres")
	}
	var users int
	if err := dst.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		return nil, err
	}
	if users > 0 {
		return nil, fmt.Errorf("target already has %d users; refusing to merge", users)
	}

	tables, err := sqliteTables(ctx, src)
	if err != nil {
		return nil, err
	}
	tx, err := dst.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SET LOCAL session_replication_role = replica`); err != nil {
		return nil, fmt.Errorf("disable FK checks for the load: %w", err)
	}

	counts := map[string]int{}
	hasID := map[string]bool{}
	for _, table := range tables {
		cols, err := pgColumns(ctx, tx, table)
		if err != nil {
			return nil, err
		}
		if len(cols) == 0 {
			continue // table the Postgres schema no longer has
		}
		n, err := copyTable(ctx, src, tx, table, cols)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", table, err)
		}
		counts[table] = n
		for _, c := range cols {
			hasID[table] = hasID[table] || c == "id"
		}
	}

	// Verify, then let the next INSERT continue after the copied ids.
	for table, want := range counts {
		var got int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil {
			return nil, err
		}
		if got != want {
			return nil, fmt.Errorf("%s: copied %d rows, target has %d", table, want, got)
		}
		if !hasID[table] {
			continue
		}
		// Any error aborts a Postgres transaction, so only ask tables that
		// have an id; non-identity ids (push_keys) come back NULL.
		var seq *string
		if err := tx.QueryRowContext(ctx, `SELECT pg_get_serial_sequence(?, 'id')`, table).Scan(&seq); err != nil {
			return nil, err
		}
		if seq != nil {
			if _, err := tx.ExecContext(ctx,
				`SELECT setval(?, COALESCE((SELECT MAX(id) FROM `+table+`), 0) + 1, false)`, *seq); err != nil {
				return nil, err
			}
		}
	}
	return counts, tx.Commit()
}

func sqliteTables(ctx context.Context, d *DB) ([]string, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// pgColumns lists the target table's columns; copying only these drops
// legacy SQLite columns the current schema no longer has.
func pgColumns(ctx context.Context, tx *Tx, table string) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT column_name FROM information_schema.columns
		 WHERE table_name = ? AND table_schema = current_schema() ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func copyTable(ctx context.Context, src *DB, tx *Tx, table string, pgCols []string) (int, error) {
	// Intersect with the source's columns (older SQLite files may lack some).
	rows, err := src.sql.QueryContext(ctx, `SELECT * FROM `+table+` LIMIT 0`)
	if err != nil {
		return 0, err
	}
	srcCols, _ := rows.Columns()
	rows.Close()
	have := map[string]bool{}
	for _, c := range srcCols {
		have[c] = true
	}
	var cols []string
	for _, c := range pgCols {
		if have[c] {
			cols = append(cols, `"`+c+`"`)
		}
	}
	list := strings.Join(cols, ", ")
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(cols)), ", ")
	insert := `INSERT INTO ` + table + ` (` + list + `) VALUES (` + marks + `)`

	rows, err = src.sql.QueryContext(ctx, `SELECT `+list+` FROM `+table)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok { // SQLite TEXT can arrive as bytes
				vals[i] = string(b)
			}
		}
		if _, err := tx.ExecContext(ctx, insert, vals...); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}
