package db

import (
	"context"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// UserPrefs is the raw JSON of a user's preferences ("" = defaults).
func (d *DB) UserPrefs(ctx context.Context, userID int64) (string, error) {
	var p string
	err := d.sql.QueryRowContext(ctx, `SELECT prefs FROM users WHERE id = ?`, userID).Scan(&p)
	return p, err
}

func (d *DB) SetUserPrefs(ctx context.Context, request appmodel.UserPrefsSaveCommand) error {
	return execRequireRows(ctx, d.sql, `UPDATE users SET prefs = ? WHERE id = ?`, request.JSON, request.UserID)
}
