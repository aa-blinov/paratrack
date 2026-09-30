package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/translit"
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
	var id int64
	err := d.sql.QueryRowContext(ctx,
		`INSERT INTO projects (team_id, slug, name, color, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		teamID, slug, name, color, now, now,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return model.Project{}, ErrDuplicate
		}
		return model.Project{}, err
	}
	if err != nil || id == 0 {
		return d.GetProjectBySlug(ctx, teamID, slug)
	}
	return d.GetProject(ctx, id)
}

// GetProject fetches one project by id without team scoping. Used by
// the picker dropdown where we already know the project is in scope.
func (d *DB) GetProject(ctx context.Context, id int64) (model.Project, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at FROM projects WHERE id = ?`, id)
	return scanProject(row)
}

// GetProjectBySlug looks up by team + slug (the URL-friendly handle).
func (d *DB) GetProjectBySlug(ctx context.Context, teamID int64, slug string) (model.Project, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at
		   FROM projects WHERE team_id = ? AND slug = ?`, teamID, strings.ToLower(slug))
	return scanProject(row)
}

// ListProjects returns projects in teamID. By default archived rows
// are hidden — set includeArchived=true to surface them too (used by
// the "Show archived" toggle on /projects).
func (d *DB) ListProjects(ctx context.Context, teamID int64, includeArchived bool) ([]model.Project, error) {
	q := `SELECT id, team_id, slug, name, color, archived, estimate_minutes, billable_rate_cents, billable, created_at, updated_at FROM projects WHERE team_id = ?`
	if !includeArchived {
		q += ` AND archived = 0`
	}
	q += ` ORDER BY archived ASC, name`
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
func (d *DB) UpdateProject(ctx context.Context, teamID, id int64, name, color string, archived *bool, estimateMinutes *int) (model.Project, error) {
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
	if estimateMinutes != nil {
		if *estimateMinutes < 0 {
			return model.Project{}, fmt.Errorf("estimate must be >= 0")
		}
		if *estimateMinutes == 0 {
			current.EstimateMinutes = nil
		} else {
			v := *estimateMinutes
			current.EstimateMinutes = &v
		}
	}
	now := FormatTime(time.Now().UTC())
	if _, err := d.sql.ExecContext(ctx,
		`UPDATE projects SET name = ?, color = ?, archived = ?, estimate_minutes = ?, updated_at = ? WHERE id = ? AND team_id = ?`,
		current.Name, current.Color, boolInt(current.Archived), current.EstimateMinutes, now, id, teamID,
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
		`UPDATE activities SET project_id = ?, updated_at = ?
		 WHERE id = ? AND team_id = ?
		 AND (project_id IS NOT DISTINCT FROM ? OR NOT EXISTS (
		   SELECT 1 FROM sessions WHERE activity_id = activities.id AND invoice_id IS NOT NULL))`,
		pid, now, activityID, teamID, pid)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		if a, err := d.GetActivity(ctx, activityID); err == nil && a.TeamID == teamID {
			return ErrAlreadyBilled
		}
		return ErrNotFound
	}
	return nil
}

// UnassignedActivity is an activity with completed time that cannot be
// invoiced until the entire activity is assigned to a billable project.
type UnassignedActivity struct {
	ID, Sessions int64
	Name         string
	Billed       bool
}

func (d *DB) UnassignedActivities(ctx context.Context, teamID int64) ([]UnassignedActivity, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT a.id, a.name, COUNT(s.id) FILTER (WHERE s.invoice_id IS NULL),
		       COUNT(s.id) FILTER (WHERE s.invoice_id IS NOT NULL)
		FROM activities a JOIN sessions s ON s.activity_id = a.id
		WHERE a.team_id = ? AND a.project_id IS NULL AND s.end_at IS NOT NULL
		GROUP BY a.id, a.name
		HAVING COUNT(s.id) FILTER (WHERE s.invoice_id IS NULL) > 0
		ORDER BY a.name`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UnassignedActivity
	for rows.Next() {
		var a UnassignedActivity
		var billed int64
		if err := rows.Scan(&a.ID, &a.Name, &a.Sessions, &billed); err != nil {
			return nil, err
		}
		a.Billed = billed > 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// AssignUnassignedActivityForBilling deliberately changes the activity,
// not just one session: all its past and future time will follow the project.
// Do not move an activity that has already been included in any invoice.
func (d *DB) AssignUnassignedActivityForBilling(ctx context.Context, teamID, activityID, projectID int64) error {
	p, err := d.GetProject(ctx, projectID)
	if err != nil || p.TeamID != teamID || p.Archived || !p.Billable || p.BillableRateCents == nil || *p.BillableRateCents <= 0 {
		return ErrNotFound
	}
	res, err := d.sql.ExecContext(ctx, `UPDATE activities SET project_id = ?, updated_at = ?
		WHERE id = ? AND team_id = ? AND project_id IS NULL
		AND NOT EXISTS (SELECT 1 FROM sessions WHERE activity_id = activities.id AND invoice_id IS NOT NULL)`,
		projectID, FormatTime(time.Now().UTC()), activityID, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
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
	q += ` ORDER BY name`
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
	if t, err := ScanTime(created); err == nil {
		p.CreatedAt = t
	}
	if t, err := ScanTime(updated); err == nil {
		p.UpdatedAt = t
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

// ProjectSpan is one closed session's tracked time, tagged with its project.
type ProjectSpan struct {
	ProjectID int64
	Session   model.Session
}

// ProjectSpans returns the closed sessions touching [from, to] that
// belong to a project, in one query for the whole workspace (scoped to
// the person for a member).
func (d *DB) ProjectSpans(ctx context.Context, teamID int64, from, to time.Time) ([]ProjectSpan, error) {
	q := `SELECT a.project_id, s.start_at, s.end_at, s.accumulated_seconds, s.paused
		FROM sessions s JOIN activities a ON a.id = s.activity_id
		WHERE s.team_id = ? AND a.project_id IS NOT NULL AND s.end_at IS NOT NULL
		  AND s.start_at <= ? AND s.end_at >= ?`
	args := []any{teamID, FormatTime(to), FormatTime(from)}
	sc, args := scopeSQL(ctx, "s.user_id", args)
	rows, err := d.sql.QueryContext(ctx, q+sc, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectSpan
	for rows.Next() {
		var sp ProjectSpan
		var st, en string
		var paused int
		if err := rows.Scan(&sp.ProjectID, &st, &en, &sp.Session.AccumulatedSeconds, &paused); err != nil {
			return nil, err
		}
		sp.Session.StartAt, _ = ScanTime(st)
		end, _ := ScanTime(en)
		sp.Session.EndAt = &end
		sp.Session.Paused = paused == 1
		out = append(out, sp)
	}
	return out, rows.Err()
}

// ProjectActivityCounts is the number of activities (archived too) per project.
func (d *DB) ProjectActivityCounts(ctx context.Context, teamID int64) (map[int64]int, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT project_id, count(*) FROM activities WHERE team_id = ? AND project_id IS NOT NULL GROUP BY project_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ProjectSessions is the project's closed sessions touching [from, to],
// newest first (scoped to the person for a member).
func (d *DB) ProjectSessions(ctx context.Context, teamID, projectID int64, from, to time.Time) ([]model.ActiveSession, error) {
	q := sessionSelect + `
		WHERE s.team_id = ? AND a.project_id = ? AND s.end_at IS NOT NULL AND s.start_at <= ? AND s.end_at >= ?`
	args := []any{teamID, projectID, FormatTime(to), FormatTime(from)}
	sc, args := scopeSQL(ctx, "s.user_id", args)
	rows, err := d.sql.QueryContext(ctx, q+sc+` ORDER BY s.start_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActiveSessions(rows)
}

// ProjectTrackedTotal is all the tracked time ever put on the project
// (closed sessions; scoped to the person for a member).
func (d *DB) ProjectTrackedTotal(ctx context.Context, teamID, projectID int64) (int, error) {
	q := `SELECT COALESCE(SUM(CASE WHEN s.accumulated_seconds > 0 THEN s.accumulated_seconds
		       ELSE GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (s.end_at::timestamp - s.start_at::timestamp)))) END), 0)::bigint
		FROM sessions s JOIN activities a ON a.id = s.activity_id
		WHERE s.team_id = ? AND a.project_id = ? AND s.end_at IS NOT NULL`
	args := []any{teamID, projectID}
	sc, args := scopeSQL(ctx, "s.user_id", args)
	var total int64
	err := d.sql.QueryRowContext(ctx, q+sc, args...).Scan(&total)
	return int(total), err
}
