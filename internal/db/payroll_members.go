package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// SetMemberPay revalidates caller permission and updates pay rate and daily
// capacity under the workspace lock. payCents = hourly pay; capacityMinutes
// = planned minutes/day (0 = 480).
func (d *DB) SetMemberPay(ctx context.Context, request appmodel.PayrollMemberPayRequest) error {
	teamID, userID, callerID := request.TeamID, request.UserID, request.CallerID
	payCents, capacityMinutes := request.PayCents, request.CapacityMinutes
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ownerID, callerRole, err := lockTeamManager(ctx, tx, teamID, callerID)
	if err != nil {
		return err
	}
	var targetRole string
	if err := tx.QueryRowContext(ctx,
		`SELECT role FROM memberships WHERE team_id = ? AND user_id = ? FOR UPDATE`, teamID, userID).Scan(&targetRole); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrNotFound
		}
		return err
	}
	if (userID == ownerID || targetRole == string(model.TeamRoleOwner)) && callerRole != model.TeamRoleOwner {
		return model.ErrForbidden
	}
	q := `UPDATE memberships SET `
	args := []any{}
	sets := []string{}
	if payCents != nil {
		if *payCents < 0 {
			return fmt.Errorf("pay must be >= 0")
		}
		sets = append(sets, "hourly_pay_cents = ?")
		args = append(args, *payCents)
	}
	if capacityMinutes != nil {
		if *capacityMinutes < 0 {
			return fmt.Errorf("capacity must be >= 0")
		}
		sets = append(sets, "capacity_minutes = ?")
		args = append(args, *capacityMinutes)
	}
	if len(sets) == 0 {
		return tx.Commit()
	}
	q += strings.Join(sets, ", ") + " WHERE team_id = ? AND user_id = ?"
	args = append(args, teamID, userID)
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrNotFound
	}
	return tx.Commit()
}

// ListMemberPayrollSettings loads compensation and capacity for every current
// member in one query for payroll roster views.
func (d *DB) ListMemberPayrollSettings(ctx context.Context, teamID int64) ([]model.MemberPayrollSettings, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT user_id, COALESCE(hourly_pay_cents, 0), COALESCE(capacity_minutes, 0)
		 FROM memberships WHERE team_id = ? ORDER BY user_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var settings []model.MemberPayrollSettings
	for rows.Next() {
		var item model.MemberPayrollSettings
		if err := rows.Scan(&item.UserID, &item.PayCents, &item.CapacityMinutes); err != nil {
			return nil, err
		}
		settings = append(settings, item)
	}
	return settings, rows.Err()
}
