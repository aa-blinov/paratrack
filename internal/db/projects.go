package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// DefaultProjectColor is the fallback tint used when a project is
// created without one. The hex format is intentional — the UI uses it
// directly as a CSS color and as the seed for ECharts palette slices.
const DefaultProjectColor = "#7c8499"

// slugRE allows letters, digits, dashes and underscores. Empty is
// rejected at the caller; this just enforces the character set.
var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// CreateProject inserts a new project in `teamID`. Pass an empty slug
// to have one auto-generated from the name. Returns ErrDuplicate if
// (team_id, slug) or (team_id, name) collides.
func (d *DB) CreateProject(ctx context.Context, teamID int64, name, slug, color string) (model.Project, error) {
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
	now := FormatTime(time.Now().UTC())
	res, err := d.sql.ExecContext(ctx,
		`INSERT INTO projects (team_id, slug, name, color, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		teamID, slug, name, color, now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return model.Project{}, ErrDuplicate
		}
		return model.Project{}, err
	}
	id, err := res.LastInsertId()
	if err != nil || id == 0 {
		return d.GetProjectBySlug(ctx, teamID, slug)
	}
	return d.GetProject(ctx, id)
}

// GetProject fetches one project by id without team scoping. Used by
// the picker dropdown where we already know the project is in scope.
func (d *DB) GetProject(ctx context.Context, id int64) (model.Project, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, created_at, updated_at FROM projects WHERE id = ?`, id)
	return scanProject(row)
}

// GetProjectBySlug looks up by team + slug (the URL-friendly handle).
func (d *DB) GetProjectBySlug(ctx context.Context, teamID int64, slug string) (model.Project, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, created_at, updated_at
		   FROM projects WHERE team_id = ? AND slug = ?`, teamID, strings.ToLower(slug))
	return scanProject(row)
}

// ListProjects returns projects in teamID. By default archived rows
// are hidden — set includeArchived=true to surface them too (used by
// the "Show archived" toggle on /projects).
func (d *DB) ListProjects(ctx context.Context, teamID int64, includeArchived bool) ([]model.Project, error) {
	q := `SELECT id, team_id, slug, name, color, archived, created_at, updated_at FROM projects WHERE team_id = ?`
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q += ` ORDER BY archived ASC, name COLLATE NOCASE`
	rows, err := d.sql.QueryContext(ctx, q, teamID)
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

// UpdateProject applies a partial update to an existing project.
// Pass empty strings / the zero value to leave a field untouched.
// Returns ErrNotFound if no row with that id exists in this team.
func (d *DB) UpdateProject(ctx context.Context, teamID, id int64, name, color string, archived *bool) (model.Project, error) {
	current, err := d.GetProject(ctx, id)
	if err != nil {
		return model.Project{}, err
	}
	if current.TeamID != teamID {
		return model.Project{}, ErrNotFound
	}
	if name != "" {
		current.Name = strings.TrimSpace(name)
	}
	if color != "" {
		if !strings.HasPrefix(color, "#") || len(color) != 7 {
			return model.Project{}, fmt.Errorf("project color %q must be a 7-char #rrggbb hex", color)
		}
		current.Color = color
	}
	if archived != nil {
		current.Archived = *archived
	}
	now := FormatTime(time.Now().UTC())
	if _, err := d.sql.ExecContext(ctx,
		`UPDATE projects SET name = ?, color = ?, archived = ?, updated_at = ? WHERE id = ?`,
		current.Name, current.Color, boolInt(current.Archived), now, id,
	); err != nil {
		return model.Project{}, err
	}
	return d.GetProject(ctx, id)
}

// DeleteProject removes a project by id. Activities that pointed at
// it are re-set to NULL via the ON DELETE SET NULL FK constraint, so
// the user's tracked time is preserved.
func (d *DB) DeleteProject(ctx context.Context, teamID, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM projects WHERE id = ? AND team_id = ?`, id, teamID)
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
	return nil
}

// AssignActivityProject sets the project_id on an activity, or clears
// it (projectID == 0). Validates that the project (if non-zero) lives
// in the same team as the activity to prevent cross-team data leak.
func (d *DB) AssignActivityProject(ctx context.Context, teamID, activityID, projectID int64) error {
	if projectID > 0 {
		p, err := d.GetProject(ctx, projectID)
		if err != nil {
			return err
		}
		if p.TeamID != teamID {
			return ErrNotFound
		}
	}
	var pid any
	if projectID > 0 {
		pid = projectID
	}
	now := FormatTime(time.Now().UTC())
	res, err := d.sql.ExecContext(ctx,
		`UPDATE activities SET project_id = ?, updated_at = ? WHERE id = ? AND team_id = ?`,
		pid, now, activityID, teamID)
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
	return nil
}

// ListActivitiesForProject returns the non-archived activities under
// a project, sorted by name. Used by /projects/{slug} to show what's
// in the project.
func (d *DB) ListActivitiesForProject(ctx context.Context, projectID int64, includeArchived bool) ([]model.Activity, error) {
	q := `SELECT id, name, team_id, project_id, archived, created_at, updated_at
	        FROM activities WHERE project_id = ?`
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := d.sql.QueryContext(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Activity
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ProjectAggregate is a per-project roll-up used by /projects to show
// "what's been tracked in this project" without a second round-trip.
type ProjectAggregate struct {
	Project           model.Project
	ActivityCount     int
	TotalSecondsAll   int // all-time
	TotalSecondsMonth int // rolling last 30 days
}

func scanProject(r row) (model.Project, error) {
	var (
		p        model.Project
		archived int
		created  string
		updated  string
	)
	if err := r.Scan(&p.ID, &p.TeamID, &p.Slug, &p.Name, &p.Color, &archived, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Project{}, ErrNotFound
		}
		return model.Project{}, err
	}
	p.Archived = archived != 0
	if t, err := ScanTime(created); err == nil {
		p.CreatedAt = t
	}
	if t, err := ScanTime(updated); err == nil {
		p.UpdatedAt = t
	}
	return p, nil
}

// boolInt converts a bool to the 0/1 used by SQLite INTEGER columns.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// slugify converts "EORA RAG v2" → "eora-rag-v2". Used only when the
// caller doesn't supply an explicit slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
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
