package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"regexp"
	"strings"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/translit"
)

// DefaultProjectColor is the fallback tint used when a project is
// created without one. The hex format is intentional — the UI uses it
// directly as a CSS color and as the seed for ECharts palette slices.
const DefaultProjectColor = "#7c8499"

// slugRE allows letters, digits, dashes and underscores. Empty is
// rejected at the caller; this just enforces the character set.
var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// CreateProjectWithBilling creates a project and applies its optional initial
// rate and currency in one transaction.
func (d *DB) CreateProjectWithBilling(ctx context.Context, request appmodel.ProjectCreateRequest) (model.Project, error) {
	teamID, callerID := request.TeamID, request.CallerID
	name, slug, color := request.Name, request.Slug, request.Color
	rateCents, currency := request.RateCents, request.Currency
	if teamID <= 0 || callerID <= 0 {
		return model.Project{}, ErrNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return model.Project{}, fmt.Errorf("project name cannot be empty")
	}
	if slug == "" {
		slug = slugify(name)
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugRE.MatchString(slug) {
		return model.Project{}, fmt.Errorf("project slug %q must be lowercase letters, digits, '-' or '_'", slug)
	}
	if color == "" {
		color = DefaultProjectColor
	}
	if !strings.HasPrefix(color, "#") || len(color) != 7 {
		return model.Project{}, fmt.Errorf("project color %q must be a 7-char #rrggbb hex", color)
	}
	if rateCents != nil && *rateCents < 0 {
		return model.Project{}, fmt.Errorf("rate must be >= 0")
	}
	if currency != "" && (len(currency) != 3 || strings.ToUpper(currency) != currency) {
		return model.Project{}, fmt.Errorf("currency must be a 3-letter uppercase code")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Project{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return model.Project{}, err
	}
	now := FormatTime(d.currentTime().UTC())
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO projects (team_id, slug, name, color, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		teamID, slug, name, color, now, now,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return model.Project{}, ErrDuplicate
		}
		return model.Project{}, err
	}
	if rateCents != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET billable_rate_cents = ? WHERE id = ? AND team_id = ?`, *rateCents, id, teamID); err != nil {
			return model.Project{}, err
		}
	}
	if currency != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET currency = ? WHERE id = ? AND team_id = ?`, currency, id, teamID); err != nil {
			return model.Project{}, err
		}
	}
	project, err := scanProject(tx.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at FROM projects WHERE id = ? AND team_id = ?`, id, teamID))
	if err != nil {
		return model.Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Project{}, err
	}
	return project, nil
}

// GetProjectByID fetches a project by its globally unique ID. It is intended
// for the local CLI, whose operator can inspect projects across workspaces.
// HTTP and application workflows should use GetProjectInTeam.
func (d *DB) GetProjectByID(ctx context.Context, id int64) (model.Project, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at FROM projects WHERE id = ?`, id)
	return scanProject(row)
}

// GetProjectInTeam fetches a project only when it belongs to teamID.
func (d *DB) GetProjectInTeam(ctx context.Context, query appmodel.ProjectScopeQuery) (model.Project, error) {
	if query.TeamID <= 0 || query.ProjectID <= 0 {
		return model.Project{}, ErrNotFound
	}
	return scanProject(d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at FROM projects WHERE id = ? AND team_id = ?`, query.ProjectID, query.TeamID))
}

func (d *DB) TeamOwnerID(ctx context.Context, teamID int64) (int64, error) {
	team, err := d.FindTeam(ctx, teamID)
	if err != nil {
		return 0, err
	}
	return team.OwnerID, nil
}

// GetProjectBySlug looks up by team + slug (the URL-friendly handle).
func (d *DB) GetProjectBySlug(ctx context.Context, query appmodel.ProjectSlugQuery) (model.Project, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at
		   FROM projects WHERE team_id = ? AND slug = ?`, query.TeamID, strings.ToLower(query.Slug))
	return scanProject(row)
}

type ProjectSummary = appmodel.ProjectSummary

// ProjectSummaries returns summaries for a set of IDs in one round trip.
func (d *DB) ProjectSummaries(ctx context.Context, query appmodel.ProjectSummariesQuery) (map[int64]ProjectSummary, error) {
	if query.TeamID <= 0 || len(query.ProjectIDs) == 0 {
		return map[int64]ProjectSummary{}, nil
	}
	sqlQuery := `SELECT id, slug, name, color FROM projects WHERE team_id = ? AND id IN (` + strings.TrimSuffix(strings.Repeat(`?,`, len(query.ProjectIDs)), `,`) + `)`
	args := make([]any, len(query.ProjectIDs)+1)
	args[0] = query.TeamID
	for i, id := range query.ProjectIDs {
		args[i+1] = id
	}
	rows, err := d.sql.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	summaries := make(map[int64]ProjectSummary, len(query.ProjectIDs))
	for rows.Next() {
		var p ProjectSummary
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Color); err != nil {
			return nil, err
		}
		summaries[p.ID] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return summaries, nil
}

// ProjectCurrencies returns explicit project currencies for one workspace.
// An empty value means the project inherits the workspace currency.
func (d *DB) ProjectCurrencies(ctx context.Context, teamID int64) (map[int64]string, error) {
	if teamID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT id, COALESCE(currency, '') FROM projects WHERE team_id = ?`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var currency string
		if err := rows.Scan(&id, &currency); err != nil {
			return nil, err
		}
		out[id] = currency
	}
	return out, rows.Err()
}

// ListProjects returns projects in teamID. By default archived rows
// are hidden — set includeArchived=true to surface them too (used by
// the "Show archived" toggle on /projects).
func (d *DB) ListProjects(ctx context.Context, query appmodel.ProjectCatalogQuery) ([]model.Project, error) {
	q := `SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at FROM projects WHERE team_id = ?`
	if !query.IncludeArchived {
		q += ` AND archived = 0`
	}
	q += ` ORDER BY archived ASC, name`
	rows, err := d.sql.QueryContext(ctx, q, query.TeamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProjectWithOptions applies core and billing fields in one transaction.
func (d *DB) UpdateProjectWithOptions(ctx context.Context, request appmodel.ProjectUpdateRequest) (model.Project, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.Project{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.Project{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.Project{}, err
	}
	current, err := scanProject(tx.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at
		 FROM projects WHERE id = ? AND team_id = ? FOR UPDATE`, request.ProjectID, request.TeamID))
	if err != nil {
		return model.Project{}, err
	}
	if request.Update.Name != "" {
		current.Name = strings.TrimSpace(request.Update.Name)
	}
	if request.Update.Color != "" {
		if !strings.HasPrefix(request.Update.Color, "#") || len(request.Update.Color) != 7 {
			return model.Project{}, fmt.Errorf("project color %q must be a 7-char #rrggbb hex", request.Update.Color)
		}
		current.Color = request.Update.Color
	}
	if request.Update.Archived != nil {
		current.Archived = *request.Update.Archived
	}
	if request.Update.EstimateMinutes != nil {
		if *request.Update.EstimateMinutes < 0 {
			return model.Project{}, fmt.Errorf("estimate must be >= 0")
		}
		if *request.Update.EstimateMinutes == 0 {
			current.EstimateMinutes = nil
		} else {
			v := *request.Update.EstimateMinutes
			current.EstimateMinutes = &v
		}
	}
	if request.Update.RateCents != nil && *request.Update.RateCents < 0 {
		return model.Project{}, fmt.Errorf("rate must be >= 0")
	}
	if request.Update.Currency != nil && *request.Update.Currency != "" && (len(*request.Update.Currency) != 3 || strings.ToUpper(*request.Update.Currency) != *request.Update.Currency) {
		return model.Project{}, fmt.Errorf("currency must be a 3-letter uppercase code")
	}
	now := FormatTime(d.currentTime().UTC())
	query := `UPDATE projects SET name = ?, color = ?, archived = ?, estimate_minutes = ?, updated_at = ?`
	args := []any{current.Name, current.Color, boolInt(current.Archived), current.EstimateMinutes, now}
	if request.Update.RateCents != nil {
		query += `, billable_rate_cents = ?`
		args = append(args, *request.Update.RateCents)
	}
	if request.Update.Billable != nil {
		query += `, billable = ?`
		args = append(args, boolInt(*request.Update.Billable))
	}
	if request.Update.Currency != nil {
		query += `, currency = ?`
		args = append(args, *request.Update.Currency)
	}
	query += ` WHERE id = ? AND team_id = ?`
	args = append(args, request.ProjectID, request.TeamID)
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return model.Project{}, err
	}
	updated, err := scanProject(tx.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at
		 FROM projects WHERE id = ? AND team_id = ?`, request.ProjectID, request.TeamID))
	if err != nil {
		return model.Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Project{}, err
	}
	return updated, nil
}

// DeleteProject removes a project by id. Activities that pointed at
// it are re-set to NULL via the ON DELETE SET NULL FK constraint, so
// the user's tracked time is preserved.
func (d *DB) DeleteProject(ctx context.Context, request appmodel.ProjectMutationRequest) error {
	if request.TeamID <= 0 || request.ProjectID <= 0 || request.CallerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ? AND team_id = ?`, request.ProjectID, request.TeamID)
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

func scanProject(r row) (model.Project, error) {
	var (
		p        model.Project
		archived int
		estimate sql.NullInt64
		rate     sql.NullInt64
		billable int
		created  string
		updated  string
	)
	if err := r.Scan(&p.ID, &p.TeamID, &p.Slug, &p.Name, &p.Color, &archived, &estimate, &rate, &billable, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Project{}, ErrNotFound
		}
		return model.Project{}, err
	}
	p.Archived = archived != 0
	if estimate.Valid {
		v := int(estimate.Int64)
		p.EstimateMinutes = &v
	}
	if rate.Valid {
		v := int(rate.Int64)
		p.BillableRateCents = &v
	}
	p.Billable = billable != 0
	createdAt, err := ScanTime(created)
	if err != nil {
		return model.Project{}, fmt.Errorf("parse project creation time: %w", err)
	}
	p.CreatedAt = createdAt
	p.UpdatedAt, err = ScanTime(updated)
	if err != nil {
		return model.Project{}, fmt.Errorf("parse project update time: %w", err)
	}
	return p, nil
}

// boolInt converts a bool to the 0/1 stored in the integer flag columns.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// slugify converts "EORA RAG v2" → "eora-rag-v2". Used only when the
// caller doesn't supply an explicit slug.
func slugify(s string) string {
	s = translit.Latin(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "project"
	}
	return out
}
