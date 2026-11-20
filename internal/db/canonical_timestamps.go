package db

import (
	"context"
	"fmt"
	"regexp"
)

// Timestamps are TEXT, and SQL compares them as strings: approval windows,
// billing periods and cursor walks all lean on lexicographic order matching
// chronological order. FormatTime now writes one fixed-width form. This
// migration rewrites the rows older binaries wrote in other shapes — the
// dropped zero fraction of RFC3339Nano, the schema's millisecond DEFAULT, the
// Python importer's space-separated form — and repoints the schema DEFAULTs,
// so one database holds one form and '.' never again sorts before 'Z'.

// canonicalTimestamp matches exactly what FormatTime writes. The value check
// happens before ParseTime so the common already-canonical row costs one
// regexp, not a parse.
var canonicalTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z$`)

// identifierPattern guards the catalog-driven SQL below: the names come from
// information_schema, but quoting rules stay explicit rather than trusted.
var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,63}$`)

// canonicalTimestampDefault is the schema DEFAULT for instant columns. To_char
// writes six microsecond digits; the literal "000" pads them to the nine the
// Go format writes, and the Z keeps the string self-describing UTC.
const canonicalTimestampDefault = `(to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US') || '000Z')`

func (d *DB) migrateCanonicalTimestamps(ctx context.Context) error {
	_, err := d.runDataMigration(ctx, "20261120_canonical_timestamps", func(tx *Tx) error {
		if err := d.normalizeTimestampColumns(ctx, tx); err != nil {
			return err
		}
		return d.canonicalizeTimestampDefaults(ctx, tx)
	})
	return err
}

// normalizeTimestampColumns finds every TEXT instant column by name (*_at,
// period_start, period_end) in the current schema and rewrites values that
// parse but are not yet canonical. The column list comes from the catalog
// rather than this file, so a column added later is covered by the same pass.
// A value that does not parse is left untouched: a row somebody corrupted
// stays corrupted instead of blocking startup.
func (d *DB) normalizeTimestampColumns(ctx context.Context, tx *Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT c.table_name, c.column_name
		  FROM information_schema.columns c
		  JOIN information_schema.tables t
		    ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		 WHERE c.table_schema = current_schema() AND t.table_type = 'BASE TABLE'
		   AND c.data_type = 'text'
		   AND (c.column_name LIKE '%\_at' OR c.column_name IN ('period_start', 'period_end'))
		 ORDER BY c.table_name, c.column_name`)
	if err != nil {
		return fmt.Errorf("list timestamp columns: %w", err)
	}
	type columnRef struct{ table, column string }
	var columns []columnRef
	for rows.Next() {
		var ref columnRef
		if err := rows.Scan(&ref.table, &ref.column); err != nil {
			rows.Close()
			return fmt.Errorf("scan timestamp column: %w", err)
		}
		if !identifierPattern.MatchString(ref.table) || !identifierPattern.MatchString(ref.column) {
			rows.Close()
			return fmt.Errorf("unexpected identifier %q.%q", ref.table, ref.column)
		}
		columns = append(columns, ref)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("walk timestamp columns: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close timestamp column list: %w", err)
	}
	for _, ref := range columns {
		if err := d.normalizeOneTimestampColumn(ctx, tx, ref.table, ref.column); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) normalizeOneTimestampColumn(ctx context.Context, tx *Tx, table, column string) error {
	values, err := tx.QueryContext(ctx,
		fmt.Sprintf(`SELECT DISTINCT "%s" FROM "%s" WHERE "%s" IS NOT NULL`, column, table, column))
	if err != nil {
		return fmt.Errorf("read %s.%s for timestamp normalisation: %w", table, column, err)
	}
	var stale []string
	for values.Next() {
		var raw string
		if err := values.Scan(&raw); err != nil {
			values.Close()
			return fmt.Errorf("scan %s.%s timestamp: %w", table, column, err)
		}
		if canonicalTimestamp.MatchString(raw) {
			continue
		}
		if _, err := ParseTime(raw); err != nil {
			continue
		}
		stale = append(stale, raw)
	}
	if err := values.Err(); err != nil {
		values.Close()
		return fmt.Errorf("walk %s.%s timestamps: %w", table, column, err)
	}
	if err := values.Close(); err != nil {
		return fmt.Errorf("close %s.%s timestamps: %w", table, column, err)
	}
	for _, raw := range stale {
		at, err := ParseTime(raw)
		if err != nil {
			// The value parsed above; refusing now would mean it changed
			// under us, which the migration lock excludes.
			return fmt.Errorf("reparse %s.%s timestamp %q: %w", table, column, raw, err)
		}
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`UPDATE "%s" SET "%s" = ? WHERE "%s" = ?`, table, column, column),
			FormatTime(at), raw); err != nil {
			return fmt.Errorf("rewrite %s.%s timestamp: %w", table, column, err)
		}
	}
	if len(stale) > 0 && d.logger != nil {
		d.logger.Printf("db: canonicalised %d distinct timestamps in %s.%s", len(stale), table, column)
	}
	return nil
}

// canonicalizeTimestampDefaults repoints the created_at/updated_at DEFAULTs
// old databases still carry. Fresh schemas get the canonical expression from
// schema.sql; existing ones need the ALTER, because a DEFAULT keeps writing
// new rows long after the stored ones were rewritten.
func (d *DB) canonicalizeTimestampDefaults(ctx context.Context, tx *Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT table_name, column_name
		  FROM information_schema.columns
		 WHERE table_schema = current_schema() AND data_type = 'text'
		   AND column_default LIKE '%to_char%' AND column_default LIKE '%now()%'
		 ORDER BY table_name, column_name`)
	if err != nil {
		return fmt.Errorf("list timestamp defaults: %w", err)
	}
	type columnRef struct{ table, column string }
	var defaults []columnRef
	for rows.Next() {
		var ref columnRef
		if err := rows.Scan(&ref.table, &ref.column); err != nil {
			rows.Close()
			return fmt.Errorf("scan timestamp default: %w", err)
		}
		if !identifierPattern.MatchString(ref.table) || !identifierPattern.MatchString(ref.column) {
			rows.Close()
			return fmt.Errorf("unexpected identifier %q.%q", ref.table, ref.column)
		}
		defaults = append(defaults, ref)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("walk timestamp defaults: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close timestamp default list: %w", err)
	}
	for _, ref := range defaults {
		if err := tx.execRawContext(ctx, fmt.Sprintf(
			`ALTER TABLE "%s" ALTER COLUMN "%s" SET DEFAULT %s`,
			ref.table, ref.column, canonicalTimestampDefault)); err != nil {
			return fmt.Errorf("repoint %s.%s default: %w", ref.table, ref.column, err)
		}
	}
	return nil
}
