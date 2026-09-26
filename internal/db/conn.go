package db

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"strings"
)

// Conn is the shared database handle. Queries are written once, in
// SQLite syntax; on Postgres every statement goes through rebind first.
// Everything else (Close, Ping, Stats) comes from the embedded *sql.DB.
type Conn struct {
	*sql.DB
	pg bool
}

// Tx is a transaction on a Conn with the same rewriting.
type Tx struct {
	*sql.Tx
	pg bool
}

func (c *Conn) q(s string) string {
	if c.pg {
		return rebind(s)
	}
	return s
}

func (t *Tx) q(s string) string {
	if t.pg {
		return rebind(s)
	}
	return s
}

func (c *Conn) Exec(q string, args ...any) (sql.Result, error) { return c.DB.Exec(c.q(q), args...) }
func (c *Conn) Query(q string, args ...any) (*sql.Rows, error) { return c.DB.Query(c.q(q), args...) }
func (c *Conn) QueryRow(q string, args ...any) *sql.Row      { return c.DB.QueryRow(c.q(q), args...) }
func (c *Conn) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return c.DB.ExecContext(ctx, c.q(q), args...)
}
func (c *Conn) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return c.DB.QueryContext(ctx, c.q(q), args...)
}
func (c *Conn) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return c.DB.QueryRowContext(ctx, c.q(q), args...)
}
func (c *Conn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := c.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx, pg: c.pg}, nil
}

func (t *Tx) Exec(q string, args ...any) (sql.Result, error) { return t.Tx.Exec(t.q(q), args...) }
func (t *Tx) Query(q string, args ...any) (*sql.Rows, error) { return t.Tx.Query(t.q(q), args...) }
func (t *Tx) QueryRow(q string, args ...any) *sql.Row      { return t.Tx.QueryRow(t.q(q), args...) }
func (t *Tx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, t.q(q), args...)
}
func (t *Tx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, t.q(q), args...)
}
func (t *Tx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, t.q(q), args...)
}

var insertOrIgnore = regexp.MustCompile(`(?is)^(\s*)INSERT\s+OR\s+IGNORE\s+INTO\b(.*?)(;?\s*)$`)

// rebind turns an SQLite-style statement into Postgres:
//   - "INSERT OR IGNORE INTO …" becomes "INSERT INTO … ON CONFLICT DO NOTHING"
//   - "COLLATE NOCASE" is dropped: those columns are CITEXT on Postgres
//   - "?" placeholders become $1, $2, … (quoted literals are left alone)
func rebind(q string) string {
	if m := insertOrIgnore.FindStringSubmatch(q); m != nil {
		q = m[1] + "INSERT INTO" + m[2] + " ON CONFLICT DO NOTHING" + m[3]
	}
	q = strings.ReplaceAll(q, " COLLATE NOCASE", "")
	if !strings.Contains(q, "?") {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 8)
	n, inQuote := 0, false
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c == '\'':
			inQuote = !inQuote
			b.WriteByte(c)
		case c == '?' && !inQuote:
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
