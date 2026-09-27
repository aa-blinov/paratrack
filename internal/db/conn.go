package db

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// Conn is the shared database handle. Queries use "?" placeholders,
// which rebind turns into Postgres $1, $2, … so dynamically built
// queries (appended filters) don't have to count arguments.
// Everything else (Close, Ping, Stats) comes from the embedded *sql.DB.
type Conn struct {
	*sql.DB
}

// Tx is a transaction on a Conn with the same rewriting.
type Tx struct {
	*sql.Tx
}

func (c *Conn) Exec(q string, args ...any) (sql.Result, error) { return c.DB.Exec(rebind(q), args...) }
func (c *Conn) Query(q string, args ...any) (*sql.Rows, error) { return c.DB.Query(rebind(q), args...) }
func (c *Conn) QueryRow(q string, args ...any) *sql.Row      { return c.DB.QueryRow(rebind(q), args...) }
func (c *Conn) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return c.DB.ExecContext(ctx, rebind(q), args...)
}
func (c *Conn) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return c.DB.QueryContext(ctx, rebind(q), args...)
}
func (c *Conn) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return c.DB.QueryRowContext(ctx, rebind(q), args...)
}
func (c *Conn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := c.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{Tx: tx}, nil
}

func (t *Tx) Exec(q string, args ...any) (sql.Result, error) { return t.Tx.Exec(rebind(q), args...) }
func (t *Tx) Query(q string, args ...any) (*sql.Rows, error) { return t.Tx.Query(rebind(q), args...) }
func (t *Tx) QueryRow(q string, args ...any) *sql.Row      { return t.Tx.QueryRow(rebind(q), args...) }
func (t *Tx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, rebind(q), args...)
}
func (t *Tx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, rebind(q), args...)
}
func (t *Tx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, rebind(q), args...)
}

// rebind turns "?" placeholders into $1, $2, … (quoted literals are left alone).
func rebind(q string) string {
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
