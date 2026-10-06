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

// savedReportSelect reads a preset next to the filters it points at. Filters
// used to be stored as the project slug and the tag name, so a rename emptied
// them until something rewrote the preset; resolving the text through the ids
// makes a rename show up on its own and a deleted project or tag read back as
// "no filter" rather than as a name nothing answers to. Every join repeats the
// workspace guard so a preset can never render another workspace's label.
const savedReportSelect = `SELECT sr.id, sr.team_id, sr.name, sr.period,
       COALESCE(p.slug, ''), COALESCE(tg.name, ''), sr.created_by, sr.created_at
  FROM saved_reports sr
  LEFT JOIN projects p ON p.id = sr.project_id AND p.team_id = sr.team_id
  LEFT JOIN tags tg ON tg.id = sr.tag_id AND tg.team_id = sr.team_id`

// CreateSavedReport inserts a preset. Duplicate names in the team are
// rejected with ErrDuplicate.
func (d *DB) CreateSavedReport(ctx context.Context, request appmodel.SavedReportCreateRequest) (SavedReport, error) {
	teamID, createdBy := request.TeamID, request.ActorID
	name, period := request.Name, request.Period
	projectSlug, tag := strings.TrimSpace(request.ProjectSlug), strings.TrimSpace(request.Tag)
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
	// The filters arrive as text from the HTTP layer, so they are turned into
	// row ids here, under the same lock as the insert. A slug or tag the
	// workspace doesn't have isn't a failure: the preset is simply saved
	// without that filter, and it starts matching again if such a row
	// reappears and the preset is saved once more.
	projectID := resolveProjectIDInTeam(ctx, tx, teamID, projectSlug)
	tagID := resolveTagIDInTeam(ctx, tx, teamID, tag)
	err = tx.QueryRowContext(ctx,
		`INSERT INTO saved_reports (team_id, name, period, project_id, tag_id, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		teamID, name, period, nullableInt64(projectID), nullableInt64(tagID), nullableInt64(createdBy), now).Scan(&id)
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

// resolveProjectIDInTeam finds the project a preset filter names, scoped to the
// preset's workspace: 0 means "no project filter". A missing slug and a lookup
// that fails outright are deliberately the same answer, because a preset must
// stay saveable even when the filter it carries points nowhere.
func resolveProjectIDInTeam(ctx context.Context, tx *Tx, teamID int64, slug string) int64 {
	if slug == "" {
		return 0
	}
	var id int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM projects WHERE team_id = ? AND slug = ?`, teamID, slug).Scan(&id); err != nil {
		return 0
	}
	return id
}

// resolveTagIDInTeam is the tag counterpart of resolveProjectIDInTeam. Global
// tags are not offered on /stats, so only the workspace's own tags match.
func resolveTagIDInTeam(ctx context.Context, tx *Tx, teamID int64, name string) int64 {
	if name == "" {
		return 0
	}
	var id int64
	if err := tx.QueryRowContext(ctx,
		`SELECT id FROM tags WHERE team_id = ? AND name = ?`, teamID, name).Scan(&id); err != nil {
		return 0
	}
	return id
}

// ListSavedReports returns the team's presets, newest first.
func (d *DB) ListSavedReports(ctx context.Context, teamID int64) ([]SavedReport, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		savedReportSelect+` WHERE sr.team_id = ? ORDER BY sr.name`, teamID)
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
	query := savedReportSelect + ` WHERE sr.id = ? AND sr.team_id = ?`
	if lock {
		// Only the preset row is locked: the joined project and tag rows are
		// read to render the filter, and locking them would serialize
		// unrelated renames and deletions against this read.
		query += ` FOR UPDATE OF sr`
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

// migrateSavedReportFilterIDs turns the filters older presets stored as text
// into the ids they name. Without it those presets have no filter at all,
// because nothing but the legacy text can say which project or tag they meant.
// A slug or tag the workspace no longer has stays NULL, so the preset reads
// back as "no filter" instead of resurrecting a name nothing answers to.
// Historical backfill; runs once.
func (d *DB) migrateSavedReportFilterIDs(ctx context.Context) error {
	var projects, tags int64
	applied, err := d.runDataMigration(ctx, "20261006_saved_report_filter_ids", func(tx *Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE saved_reports sr SET project_id = p.id
			   FROM projects p
			  WHERE sr.project_id IS NULL AND sr.project_slug <> ''
			    AND p.team_id = sr.team_id AND p.slug = sr.project_slug`)
		if err != nil {
			return fmt.Errorf("resolve saved report project filters: %w", err)
		}
		if projects, err = res.RowsAffected(); err != nil {
			return err
		}
		res, err = tx.ExecContext(ctx,
			`UPDATE saved_reports sr SET tag_id = t.id
			   FROM tags t
			  WHERE sr.tag_id IS NULL AND sr.tag <> ''
			    AND t.team_id = sr.team_id AND t.name = sr.tag`)
		if err != nil {
			return fmt.Errorf("resolve saved report tag filters: %w", err)
		}
		tags, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return err
	}
	if applied && (projects > 0 || tags > 0) {
		d.logger.Printf("saved reports: %d project and %d tag filters stored as text resolved to rows", projects, tags)
	}
	return nil
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
