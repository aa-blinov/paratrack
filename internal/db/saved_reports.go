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

// Saved-report persistence and actor-scoped authorization.
// ---------------------------------------------------------------------------
// Saved reports (Wave 1)
// ---------------------------------------------------------------------------

// SavedReport is an alias to the shared report preset model.
type SavedReport = model.SavedReport

// CreateSavedReport inserts a preset. Duplicate names in the team are
// rejected with ErrDuplicate.
func (d *DB) CreateSavedReport(ctx context.Context, request appmodel.SavedReportCreateRequest) (SavedReport, error) {
	teamID, createdBy := request.TeamID, request.ActorID
	name, period := request.Name, request.Period
	projectSlug, tag := request.ProjectSlug, request.Tag
	if teamID <= 0 || createdBy <= 0 {
		return SavedReport{}, model.ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return SavedReport{}, fmt.Errorf("name is required")
	}
	if period == "" {
		period = "today"
	}
	now := FormatTime(d.currentTime().UTC())
	var id int64
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return SavedReport{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, teamID, createdBy); err != nil {
		return SavedReport{}, err
	}
	err = tx.QueryRowContext(ctx,
		`INSERT INTO saved_reports (team_id, name, period, project_slug, tag, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		teamID, name, period, projectSlug, tag, nullableInt64(createdBy), now).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return SavedReport{}, ErrDuplicate
		}
		return SavedReport{}, err
	}
	report, err := getSavedReport(ctx, tx, teamID, id, true)
	if err != nil {
		return SavedReport{}, err
	}
	if err := tx.Commit(); err != nil {
		return SavedReport{}, err
	}
	return report, nil
}

// ListSavedReports returns the team's presets, newest first.
func (d *DB) ListSavedReports(ctx context.Context, teamID int64) ([]SavedReport, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, name, period, project_slug, tag, created_by, created_at
		 FROM saved_reports WHERE team_id = ? ORDER BY name`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedReport
	for rows.Next() {
		r, err := scanSavedReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func getSavedReport(ctx context.Context, queryer queryRower, teamID, id int64, lock bool) (SavedReport, error) {
	query := `SELECT id, team_id, name, period, project_slug, tag, created_by, created_at
		 FROM saved_reports WHERE id = ? AND team_id = ?`
	if lock {
		query += ` FOR UPDATE`
	}
	row := queryer.QueryRowContext(ctx, query, id, teamID)
	r, err := scanSavedReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SavedReport{}, ErrNotFound
	}
	return r, err
}

// DeleteSavedReportForActor removes a preset after rechecking the caller's
// current workspace role under the same lock as the ownership-scoped delete.
func (d *DB) DeleteSavedReportForActor(ctx context.Context, request appmodel.SavedReportDeleteRequest) error {
	if request.TeamID <= 0 || request.ReportID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	teamID, id, actorID := request.TeamID, request.ReportID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	ownerID, role, err := lockTeamRole(ctx, tx, teamID, actorID)
	if err != nil {
		return err
	}
	if actorID == ownerID && role != model.TeamRoleOwner {
		return model.ErrForbidden
	}
	q := `DELETE FROM saved_reports WHERE id = ? AND team_id = ?`
	args := []any{id, teamID}
	if !role.CanManage() {
		q += ` AND created_by = ?`
		args = append(args, actorID)
	}
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func scanSavedReport(r interface{ Scan(...any) error }) (SavedReport, error) {
	var (
		out       SavedReport
		createdBy sql.NullInt64
		created   string
	)
	if err := r.Scan(&out.ID, &out.TeamID, &out.Name, &out.Period,
		&out.ProjectSlug, &out.Tag, &createdBy, &created); err != nil {
		return SavedReport{}, err
	}
	if createdBy.Valid {
		out.CreatedBy = createdBy.Int64
	}
	createdAt, err := ScanTime(created)
	if err != nil {
		return SavedReport{}, fmt.Errorf("parse saved report creation time: %w", err)
	}
	out.CreatedAt = createdAt
	return out, nil
}
