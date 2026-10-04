package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

// ---------------------------------------------------------------------------
// Timesheet (Wave 1)
//
// A week-grid of activity × day totals. Reads aggregate closed (+ active)
// sessions into per-day tracked seconds. Writes go through
// UpsertDayTotal which owns exactly one closed "sheet" session per
// (activity, day) so the cell value is the source of truth.
// ---------------------------------------------------------------------------

type DayCell = model.TimesheetCell
type TimesheetWeek = model.TimesheetWeek

// sheetAllRows: up to this many activities, every one gets a row.
const sheetAllRows = 30

type timesheetAccumulator struct {
	name   string
	projID int64
	secs   [7]int
	total  int
}

// ListTimesheet aggregates tracked time per (activity, day) inside
// [weekStart, weekStart+7d). Sessions are clipped to the week window
// and scaled like TrackedSecondsInWindow so pause gaps don't inflate
// the cell.
func (d *DB) ListTimesheet(ctx context.Context, request appmodel.TimesheetRequest) (TimesheetWeek, error) {
	if request.TeamID <= 0 || request.WeekStart.IsZero() || request.Now.IsZero() {
		return TimesheetWeek{}, ErrNotFound
	}
	for _, activityID := range request.ExtraActivityIDs {
		if activityID <= 0 {
			return TimesheetWeek{}, ErrNotFound
		}
	}
	weekEnd := request.WeekStart.AddDate(0, 0, 7)
	query := `
		SELECT s.activity_id, s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at,
		       a.name, a.project_id
		FROM sessions s
		JOIN activities a ON a.id = s.activity_id AND a.team_id = s.team_id
		WHERE s.start_at < ?
		  AND (s.end_at IS NULL OR s.end_at >= ?)`
	args := []any{FormatTime(weekEnd), FormatTime(request.WeekStart)}
	query += ` AND s.team_id = ?`
	args = append(args, request.TeamID)
	scope, args := scopeSQL(ctx, "s.user_id", args)
	buckets, err := d.aggregateTimesheetSessions(ctx, query+scope, args, request.WeekStart, request.Now)
	if err != nil {
		return TimesheetWeek{}, err
	}

	week, err := summarizeTimesheetBuckets(buckets)
	if err != nil {
		return TimesheetWeek{}, err
	}
	if err := d.appendTimesheetActivities(ctx, &week, request.TeamID, request.WeekStart, request.ExtraActivityIDs); err != nil {
		return TimesheetWeek{}, err
	}
	return week, nil
}

func (d *DB) aggregateTimesheetSessions(ctx context.Context, query string, args []any, weekStart, now time.Time) (map[int64]*timesheetAccumulator, error) {
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets := make(map[int64]*timesheetAccumulator)
	for rows.Next() {
		var (
			activityID                 int64
			startAt, endAt, lastResume sql.NullString
			accumulated                int
			paused                     int
			name                       string
			projectID                  sql.NullInt64
		)
		if err := rows.Scan(&activityID, &startAt, &endAt, &accumulated, &paused, &lastResume, &name, &projectID); err != nil {
			return nil, err
		}
		started, err := ScanTime(startAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse timesheet session start: %w", err)
		}
		session := model.Session{
			StartAt: started, AccumulatedSeconds: accumulated,
			Paused: paused == 1, ActivityID: activityID,
		}
		if endAt.Valid {
			ended, err := ScanTime(endAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse timesheet session end: %w", err)
			}
			session.EndAt = &ended
		}
		if lastResume.Valid {
			resumed, err := ScanTime(lastResume.String)
			if err != nil {
				return nil, fmt.Errorf("parse timesheet session resume time: %w", err)
			}
			session.LastResumeAt = &resumed
		}
		bucket := buckets[activityID]
		if bucket == nil {
			project := int64(0)
			if projectID.Valid {
				project = projectID.Int64
			}
			bucket = &timesheetAccumulator{name: name, projID: project}
			buckets[activityID] = bucket
		}
		for day := 0; day < 7; day++ {
			dayStart := weekStart.AddDate(0, 0, day)
			dayEnd := dayStart.AddDate(0, 0, 1)
			seconds := session.TrackedSecondsInWindow(dayStart, dayEnd, now)
			if seconds > 0 {
				bucket.secs[day], err = money.AddInt(bucket.secs[day], seconds)
				if err != nil {
					return nil, fmt.Errorf("sum timesheet activity %d day %d: %w", activityID, day, err)
				}
				bucket.total, err = money.AddInt(bucket.total, seconds)
				if err != nil {
					return nil, fmt.Errorf("sum timesheet activity %d: %w", activityID, err)
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buckets, nil
}

func summarizeTimesheetBuckets(buckets map[int64]*timesheetAccumulator) (TimesheetWeek, error) {
	var week TimesheetWeek
	for id, bucket := range buckets {
		if bucket.total <= 0 {
			continue
		}
		week.Rows = append(week.Rows, DayCell{
			ActivityID: id, ActivityName: bucket.name, ProjectID: bucket.projID,
			Secs: bucket.secs, RowTotal: bucket.total,
		})
		for day, seconds := range bucket.secs {
			var err error
			week.DayTotals[day], err = money.AddInt(week.DayTotals[day], seconds)
			if err != nil {
				return TimesheetWeek{}, fmt.Errorf("sum timesheet day %d: %w", day, err)
			}
		}
		var err error
		week.GrandTotal, err = money.AddInt(week.GrandTotal, bucket.total)
		if err != nil {
			return TimesheetWeek{}, fmt.Errorf("sum timesheet week: %w", err)
		}
	}
	sort.Slice(week.Rows, func(i, j int) bool {
		left, right := strings.ToLower(week.Rows[i].ActivityName), strings.ToLower(week.Rows[j].ActivityName)
		if left != right {
			return left < right
		}
		return week.Rows[i].ActivityID < week.Rows[j].ActivityID
	})
	return week, nil
}

func (d *DB) appendTimesheetActivities(ctx context.Context, week *TimesheetWeek, teamID int64, weekStart time.Time, extra []int64) error {
	if teamID <= 0 {
		return nil
	}
	activities, err := d.ListActivities(ctx, teamID, false)
	if err != nil {
		return fmt.Errorf("list timesheet activities: %w", err)
	}
	seen := make(map[int64]bool, len(week.Rows))
	for _, row := range week.Rows {
		seen[row.ActivityID] = true
	}
	keep := make(map[int64]bool, len(extra))
	for _, id := range extra {
		keep[id] = true
	}
	if len(activities) > sheetAllRows {
		recent, err := d.recentTimesheetActivityIDs(ctx, teamID, weekStart)
		if err != nil {
			return err
		}
		for id := range recent {
			keep[id] = true
		}
	}
	for _, activity := range activities {
		if seen[activity.ID] {
			continue
		}
		if len(activities) <= sheetAllRows || keep[activity.ID] {
			week.Rows = append(week.Rows, DayCell{ActivityID: activity.ID, ActivityName: activity.Name, ProjectID: activity.ProjectID})
		} else {
			week.Others = append(week.Others, activity)
		}
	}
	return nil
}

func (d *DB) recentTimesheetActivityIDs(ctx context.Context, teamID int64, weekStart time.Time) (map[int64]bool, error) {
	query := `SELECT DISTINCT activity_id FROM sessions WHERE team_id = ? AND start_at >= ? AND start_at < ?`
	args := []any{teamID, FormatTime(weekStart.AddDate(0, 0, -56)), FormatTime(weekStart.AddDate(0, 0, 7))}
	scope, args := scopeSQL(ctx, "user_id", args)
	rows, err := d.sql.QueryContext(ctx, query+scope, args...)
	if err != nil {
		return nil, fmt.Errorf("find recently used timesheet activities: %w", err)
	}
	defer rows.Close()
	ids := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan recently used timesheet activity: %w", err)
		}
		ids[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recently used timesheet activities: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close recently used timesheet activities: %w", err)
	}
	return ids, nil
}

// UpsertDayTotal atomically replaces the current actor's total for an
// activity and day with one synthetic session (or clears the cell). It locks
// the activity and affected sessions and refuses sent or paid invoice rows.

func (d *DB) UpsertDayTotal(ctx context.Context, request appmodel.TimesheetCellUpdateRequest) error {
	teamID, activityID, day, totalSecs := request.TeamID, request.ActivityID, request.Day, request.TotalSeconds
	if teamID <= 0 {
		return model.ErrForbidden
	}
	if activityID <= 0 || totalSecs < 0 || totalSecs > 24*60*60 {
		return fmt.Errorf("invalid timesheet cell update")
	}
	actor := actorID(ctx)
	if actor <= 0 {
		return model.ErrForbidden
	}
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.AddDate(0, 0, 1)
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockSessionOwner(ctx, tx, teamID); err != nil {
		return err
	}
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return err
	}
	if err := lockActivityForSession(ctx, tx, teamID, activityID); err != nil {
		return err
	}
	if teamID > 0 {
		if err := lockTimesheetCellInvoices(ctx, tx, teamID, activityID, dayStart, dayEnd); err != nil {
			return err
		}
	}

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
	// Only the actor's own day: a colleague's sessions on the same
	// activity are theirs, not this cell's.
	del += ` AND user_id = ?`
	delArgs = append(delArgs, actor)
	if _, err := tx.ExecContext(ctx, del, delArgs...); err != nil {
		return err
	}
	if totalSecs == 0 {
		return tx.Commit()
	}
	// One sheet session 09:00 → 09:00+total.
	start := dayStart.Add(9 * time.Hour)
	end := start.Add(time.Duration(totalSecs) * time.Second)
	if _, err := insertClosedSessionTx(ctx, tx, teamID, activityID, start, end, "timesheet"); err != nil {
		return err
	}
	return tx.Commit()
}

func lockTimesheetCellInvoices(ctx context.Context, tx *Tx, teamID, activityID int64, dayStart, dayEnd time.Time) error {
	query := `SELECT COALESCE(s.invoice_id, 0)
		FROM sessions s
		WHERE s.team_id = ? AND s.activity_id = ? AND s.end_at IS NOT NULL
		  AND s.start_at >= ? AND s.start_at < ? AND (? = 0 OR s.user_id = ?)
		ORDER BY s.id FOR UPDATE OF s`
	actor := actorID(ctx)
	rows, err := tx.QueryContext(ctx, query,
		teamID, activityID, FormatTime(dayStart), FormatTime(dayEnd), actor, actor)
	if err != nil {
		return err
	}
	defer rows.Close()
	invoiceIDs := make([]int64, 0)
	seen := make(map[int64]struct{})
	for rows.Next() {
		var invoiceID int64
		if err := rows.Scan(&invoiceID); err != nil {
			rows.Close()
			return err
		}
		if invoiceID > 0 {
			if _, ok := seen[invoiceID]; !ok {
				seen[invoiceID] = struct{}{}
				invoiceIDs = append(invoiceIDs, invoiceID)
			}
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	sort.Slice(invoiceIDs, func(i, j int) bool { return invoiceIDs[i] < invoiceIDs[j] })
	if len(invoiceIDs) == 0 {
		return nil
	}
	invoiceRows, err := tx.QueryContext(ctx,
		`SELECT number, status FROM invoices WHERE id = ANY(?) ORDER BY id FOR UPDATE`, invoiceIDs)
	if err != nil {
		return err
	}
	defer invoiceRows.Close()
	for invoiceRows.Next() {
		var number, status string
		if err := invoiceRows.Scan(&number, &status); err != nil {
			return err
		}
		if status == "sent" || status == "paid" {
			return &model.SessionInvoiceLockError{InvoiceNumber: number}
		}
	}
	return invoiceRows.Err()
}
