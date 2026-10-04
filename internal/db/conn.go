package db

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// Conn is the shared database handle. It deliberately exposes only
// context-aware operations. Queries use "?" placeholders, which rebind turns
// into Postgres $1, $2, … so dynamically built queries don't count arguments.
type Conn struct{ db *sql.DB }

// Tx is a transaction on a Conn with the same context and rebinding rules.
type Tx struct{ tx *sql.Tx }

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (c *Conn) Close() error                          { return c.db.Close() }
func (c *Conn) Stats() sql.DBStats                    { return c.db.Stats() }
func (c *Conn) PingContext(ctx context.Context) error { return c.db.PingContext(ctx) }

func (c *Conn) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return c.db.ExecContext(ctx, rebind(q), args...)
}
func (c *Conn) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return c.db.QueryContext(ctx, rebind(q), args...)
}
func (c *Conn) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return c.db.QueryRowContext(ctx, rebind(q), args...)
}
func (c *Conn) BeginTx(ctx context.Context, opts *sql.TxOptions) (*Tx, error) {
	tx, err := c.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx}, nil
}

func (t *Tx) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, rebind(q), args...)
}
func (t *Tx) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, rebind(q), args...)
}
func (t *Tx) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, rebind(q), args...)
}

func (t *Tx) execRawContext(ctx context.Context, q string) error {
	_, err := t.tx.ExecContext(ctx, q)
	return err
}

func (t *Tx) Commit() error   { return t.tx.Commit() }
func (t *Tx) Rollback() error { return t.tx.Rollback() }

// rebind turns "?" placeholders into $1, $2, … (quoted literals are left alone).
func rebind(q string) string {
	if !strings.Contains(q, "?") {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 0
	for i := 0; i < len(q); {
		switch {
		case q[i] == '\'':
			// E'...' strings accept backslash escapes; ordinary SQL strings
			// escape a quote by doubling it.
			escaped := i > 0 && (q[i-1] == 'E' || q[i-1] == 'e') &&
				(i == 1 || !isSQLIdentifierByte(q[i-2]))
			b.WriteByte(q[i])
			i++
			for i < len(q) {
				if escaped && q[i] == '\\' && i+1 < len(q) {
					b.WriteString(q[i : i+2])
					i += 2
					continue
				}
				b.WriteByte(q[i])
				if q[i] == '\'' {
					if i+1 < len(q) && q[i+1] == '\'' {
						b.WriteByte(q[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case q[i] == '"':
			b.WriteByte(q[i])
			i++
			for i < len(q) {
				b.WriteByte(q[i])
				if q[i] == '"' {
					if i+1 < len(q) && q[i+1] == '"' {
						b.WriteByte(q[i+1])
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
		case strings.HasPrefix(q[i:], "--"):
			end := strings.IndexByte(q[i:], '\n')
			if end < 0 {
				b.WriteString(q[i:])
				return b.String()
			}
			end += i
			b.WriteString(q[i : end+1])
			i = end + 1
		case strings.HasPrefix(q[i:], "/*"):
			start, depth := i, 0
			for i < len(q) {
				switch {
				case strings.HasPrefix(q[i:], "/*"):
					depth++
					i += 2
				case strings.HasPrefix(q[i:], "*/"):
					depth--
					i += 2
					if depth == 0 {
						b.WriteString(q[start:i])
						goto next
					}
				default:
					i++
				}
			}
			b.WriteString(q[start:])
			return b.String()
		case q[i] == '$':
			startsDollarQuote := i == 0 || !isSQLIdentifierByte(q[i-1])
			if delimiter, ok := dollarQuoteDelimiter(q[i:]); ok && startsDollarQuote {
				b.WriteString(delimiter)
				i += len(delimiter)
				end := strings.Index(q[i:], delimiter)
				if end < 0 {
					b.WriteString(q[i:])
					return b.String()
				}
				end += i + len(delimiter)
				b.WriteString(q[i:end])
				i = end
			} else {
				b.WriteByte(q[i])
				i++
			}
		case q[i] == '?':
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			i++
		default:
			b.WriteByte(q[i])
			i++
		}
		continue
	next:
	}
	return b.String()
}

func isSQLIdentifierByte(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func dollarQuoteDelimiter(s string) (string, bool) {
	if len(s) < 2 || s[0] != '$' {
		return "", false
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '$' {
			return s[:i+1], true
		}
		if !(s[i] == '_' || s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z' || i > 1 && s[i] >= '0' && s[i] <= '9') {
			return "", false
		}
	}
	return "", false
}
