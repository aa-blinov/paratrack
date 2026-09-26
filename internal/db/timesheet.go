package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------
// Timesheet (Wave 1)
//
// A week-grid of activity × day totals. Reads aggregate closed (+ active)
// sessions into per-day tracked seconds. Writes go through
// UpsertDayTotal which owns exactly one closed "sheet" session per
// (activity, day) so the cell value is the source of truth.
// ---------------------------------------------------------------------------

// DayCell is one grid cell: tracked seconds for an activity on a date.
type DayCell struct {
	ActivityID   int64
	ActivityName string
	Color        string // filled by the web layer
	ProjectID    int64
	// Secs is indexed Mon..Sun (0..6).
	Secs     [7]int
	RowTotal int
}

// TimesheetWeek is the full grid for one week.
type TimesheetWeek struct {
	Rows       []DayCell
	DayTotals  [7]int
	GrandTotal int
}

// ListTimesheet aggregates tracked time per (activity, day) inside
// [weekStart, weekStart+7d). Sessions are clipped to the week window
// and scaled like TrackedSecondsInWindow so pause gaps don't inflate
// the cell.
func (d *DB) ListTimesheet(ctx context.Context, teamID int64, weekStart time.Time, now time.Time) (TimesheetWeek, error) {
	weekEnd := weekStart.AddDate(0, 0, 7)
	q := `
		SELECT s.activity_id, s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at,
		       a.name, a.project_id
		FROM sessions s
		JOIN activities a ON a.id = s.activity_id
		WHERE s.start_at < ?
		  AND (s.end_at IS NULL OR s.end_at >= ?)`
	args := []any{FormatTime(weekEnd), FormatTime(weekStart)}
	if teamID > 0 {
		q += ` AND s.team_id = ?`
		args = append(args, teamID)
	}
	q += ` ORDER BY a.name COLLATE NOCASE, s.start_at`

	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return TimesheetWeek{}, err
	}
	defer rows.Close()

	type acc struct {
		name   string
		projID int64
		secs   [7]int
		total  int
	}
	buckets := map[int64]*acc{}

	for rows.Next() {
		var (
			actID                      int64
			startAt, endAt, lastResume sql.NullString
			accum                      int
			paused                     int
			name                       string
			projID                     sql.NullInt64
		)
		if err := rows.Scan(&actID, &startAt, &endAt, &accum, &paused, &lastResume, &name, &projID); err != nil {
			return TimesheetWeek{}, err
		}
		st, err := ScanTime(startAt.String)
		if err != nil {
			continue
		}
		sess := model.Session{
			StartAt:            st,
			AccumulatedSeconds: accum,
			Paused:             paused == 1,
			ActivityID:         actID,
		}
		if endAt.Valid {
			if t, err := ScanTime(endAt.String); err == nil {
				sess.EndAt = &t
			}
		}
		if lastResume.Valid {
			if t, err := ScanTime(lastResume.String); err == nil {
				sess.LastResumeAt = &t
			}
		}
		b := buckets[actID]
		if b == nil {
			pid := int64(0)
			if projID.Valid {
				pid = projID.Int64
			}
			b = &acc{name: name, projID: pid}
			buckets[actID] = b
		}
		// Attribute to each weekday the session overlaps.
		for i := 0; i < 7; i++ {
			dayStart := weekStart.AddDate(0, 0, i)
			dayEnd := dayStart.AddDate(0, 0, 1)
			sec := sess.TrackedSecondsInWindow(dayStart, dayEnd, now)
			if sec > 0 {
				b.secs[i] += sec
				b.total += sec
			}
		}
	}
	if err := rows.Err(); err != nil {
		return TimesheetWeek{}, err
	}

	var out TimesheetWeek
	for id, b := range buckets {
		if b.total <= 0 {
			continue
		}
		row := DayCell{
			ActivityID:   id,
			ActivityName: b.name,
			ProjectID:    b.projID,
			Secs:         b.secs,
			RowTotal:     b.total,
		}
		for i := 0; i < 7; i++ {
			out.DayTotals[i] += b.secs[i]
		}
		out.GrandTotal += b.total
		out.Rows = append(out.Rows, row)
	}
	// buckets is a map: without this the rows came back in a new random
	// order on every load.
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := strings.ToLower(out.Rows[i].ActivityName), strings.ToLower(out.Rows[j].ActivityName)
		if a != b {
			return a < b
		}
		return out.Rows[i].ActivityID < out.Rows[j].ActivityID
	})
	// Also include every activity that has no time this week so the
	// user can fill an empty row (stable order by name).
	if teamID > 0 {
		acts, err := d.ListActivities(ctx, teamID, false)
		if err == nil {
			seen := map[int64]bool{}
			for _, r := range out.Rows {
				seen[r.ActivityID] = true
			}
			for _, a := range acts {
				if !seen[a.ID] {
					out.Rows = append(out.Rows, DayCell{
						ActivityID:   a.ID,
						ActivityName: a.Name,
						ProjectID:    a.ProjectID,
					})
				}
			}
		}
	}
	return out, nil
}

// UpsertDayTotal sets the tracked total for (activity, day) to the
// given duration. It collapses existing closed sessions that day into
// one synthetic "sheet" session anchored at 09:00, or deletes them all
// when totalSecs is 0.
func (d *DB) UpsertDayTotal(ctx context.Context, teamID, activityID int64, day time.Time, totalSecs int) error {
	if totalSecs < 0 {
		return fmt.Errorf("duration must be >= 0")
	}
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.AddDate(0, 0, 1)

	// Drop existing closed sessions for this activity/day — the cell is
	// the source of truth for the sheet.
	del := `DELETE FROM sessions
	        WHERE activity_id = ?
	          AND end_at IS NOT NULL
	          AND start_at >= ?
	          AND start_at < ?`
	delArgs := []any{activityID, FormatTime(dayStart), FormatTime(dayEnd)}
	if teamID > 0 {
		del += ` AND team_id = ?`
		delArgs = append(delArgs, teamID)
	}
	if _, err := d.sql.ExecContext(ctx, del, delArgs...); err != nil {
		return err
	}
	if totalSecs == 0 {
		return nil
	}
	// One sheet session 09:00 → 09:00+total.
	start := dayStart.Add(9 * time.Hour)
	end := start.Add(time.Duration(totalSecs) * time.Second)
	note := "timesheet"
	_, err := d.CreateClosedSession(ctx, teamID, activityID, start, end, note)
	return err
}

// ---------------------------------------------------------------------------
// Saved reports (Wave 1)
// ---------------------------------------------------------------------------

// SavedReport is a named /stats filter preset.
type SavedReport struct {
	ID          int64     `json:"id"`
	TeamID      int64     `json:"team_id"`
	Name        string    `json:"name"`
	Period      string    `json:"period"`
	ProjectSlug string    `json:"project_slug"`
	Tag         string    `json:"tag"`
	CreatedBy   int64     `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateSavedReport inserts a preset. Duplicate names in the team are
// rejected with ErrDuplicate.
func (d *DB) CreateSavedReport(ctx context.Context, teamID int64, name, period, projectSlug, tag string, createdBy int64) (SavedReport, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SavedReport{}, fmt.Errorf("name is required")
	}
	if period == "" {
		period = "today"
	}
	now := FormatTime(time.Now().UTC())
	var id int64
	err := d.sql.QueryRowContext(ctx,
		`INSERT INTO saved_reports (team_id, name, period, project_slug, tag, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		teamID, name, period, projectSlug, tag, nullableInt64(createdBy), now).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return SavedReport{}, ErrDuplicate
		}
		return SavedReport{}, err
	}
	return d.GetSavedReport(ctx, teamID, id)
}

// ListSavedReports returns the team's presets, newest first.
func (d *DB) ListSavedReports(ctx context.Context, teamID int64) ([]SavedReport, error) {
	q := `SELECT id, team_id, name, period, project_slug, tag, created_by, created_at
	      FROM saved_reports`
	args := []any{}
	if teamID > 0 {
		q += ` WHERE team_id = ?`
		args = append(args, teamID)
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := d.sql.QueryContext(ctx, q, args...)
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

// GetSavedReport fetches one preset inside a team.
func (d *DB) GetSavedReport(ctx context.Context, teamID, id int64) (SavedReport, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, name, period, project_slug, tag, created_by, created_at
		 FROM saved_reports WHERE id = ? AND team_id = ?`, id, teamID)
	r, err := scanSavedReport(row)
	if errors.Is(err, sql.ErrNoRows) {
		return SavedReport{}, ErrNotFound
	}
	return r, err
}

// DeleteSavedReport removes a preset.
func (d *DB) DeleteSavedReport(ctx context.Context, teamID, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM saved_reports WHERE id = ? AND team_id = ?`, id, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
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
	out.CreatedAt, _ = ScanTime(created)
	return out, nil
}

// nullableInt64 helper (also used by sessions).
var _ = nullableInt64
