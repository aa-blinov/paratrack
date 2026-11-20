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

type DayCell = appmodel.TimesheetCell
type TimesheetWeek = appmodel.TimesheetWeek

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
		       a.name, ` + sessionProject + `
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

// UpsertDayTotal atomically sets the current actor's total for an activity and
// day: the sheet session takes the difference over the logged intervals, and a
// total below them is refused (model.ErrCellBelowIntervals). It locks
// the activity and affected sessions and refuses sent or paid invoice rows.
func (d *DB) UpsertDayTotal(ctx context.Context, request appmodel.TimesheetCellUpdateRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := upsertDayTotalTx(ctx, d, tx, request.TeamID, request.ActivityID, request.Day, request.TotalSeconds); err != nil {
		return err
	}
	return tx.Commit()
}

// upsertDayTotalTx is the cell write inside a caller's transaction.
// DecideTimeEditRequest claims a request and writes the approved cell as one
// step, so a refusal leaves nothing behind and two concurrent decisions
// cannot both act (TIME-19).
func upsertDayTotalTx(ctx context.Context, d *DB, tx *Tx, teamID, activityID int64, day time.Time, totalSecs int) error {
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
	if err := lockApprovedSpan(ctx, tx, teamID, dayStart, dayEnd); err != nil {
		return err
	}
	if err := lockAutoClosed(ctx, tx, teamID, dayStart, d.currentTime()); err != nil {
		return err
	}
	if err := lockClosedPeriodSpan(ctx, tx, teamID, dayStart, dayEnd); err != nil {
		return err
	}
	if err := lockSessionOwner(ctx, tx, teamID); err != nil {
		return err
	}
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return err
	}
	// The day's sessions are locked before the activity row, in id order,
	// the same order the invoice builder locks (sessions, then activity):
	// the opposite order is a deadlock between a cell write and a draft over
	// the same day. The lock also closes the intervals race: a stop or an
	// edit committing between the read below and the write would otherwise
	// push the day past what the cell counted.
	daySessions, err := tx.QueryContext(ctx,
		`SELECT id FROM sessions
		  WHERE activity_id = ? AND team_id = ? AND user_id = ?
		    AND end_at IS NOT NULL AND start_at >= ? AND start_at < ?
		  ORDER BY id FOR UPDATE`,
		activityID, teamID, actor, FormatTime(dayStart), FormatTime(dayEnd))
	if err != nil {
		return err
	}
	for daySessions.Next() {
		var lockedID int64
		if err := daySessions.Scan(&lockedID); err != nil {
			daySessions.Close()
			return err
		}
	}
	if err := daySessions.Err(); err != nil {
		daySessions.Close()
		return err
	}
	if err := daySessions.Close(); err != nil {
		return err
	}
	if err := lockActivityForSession(ctx, tx, teamID, activityID); err != nil {
		return err
	}
	if teamID > 0 {
		if err := lockTimesheetInvoiceLocks(ctx, tx, teamID, activityID, dayStart, dayEnd); err != nil {
			return err
		}
	}

	// The cell only raises a day. Closed sessions with real intervals (a start and an
	// end the person logged) stay as they are; the sheet session holds the difference.
	// A total below what those intervals already hold would have to erase a start or
	// an end, so it is refused. Only the actor's own day counts: a colleague's
	// sessions on the same activity are theirs, not this cell's.
	var realSecs int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(accumulated_seconds), 0) FROM sessions
		  WHERE activity_id = ? AND team_id = ? AND user_id = ?
		    AND end_at IS NOT NULL AND start_at >= ? AND start_at < ?
		    AND COALESCE(note, '') <> 'timesheet'`,
		activityID, teamID, actor, FormatTime(dayStart), FormatTime(dayEnd)).Scan(&realSecs); err != nil {
		return err
	}
	if int64(totalSecs) < realSecs {
		return model.ErrCellBelowIntervals
	}
	// The earlier sheet session of this cell is replaced; nothing else is.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions
		  WHERE activity_id = ? AND team_id = ? AND user_id = ?
		    AND end_at IS NOT NULL AND start_at >= ? AND start_at < ?
		    AND note = 'timesheet'`,
		activityID, teamID, actor, FormatTime(dayStart), FormatTime(dayEnd)); err != nil {
		return err
	}
	remainder := int64(totalSecs) - realSecs
	if remainder == 0 {
		return nil
	}
	// One sheet session 09:00 → 09:00+remainder. A remainder above fifteen
	// hours would push that anchor past midnight, and the grid files a
	// session under the day it starts on, so the surplus would surface on
	// the next day. A large remainder anchors back from the end of the day
	// instead: the session stays inside the day the cell belongs to.
	start := dayStart.Add(9 * time.Hour)
	if reach := start.Add(time.Duration(remainder) * time.Second); reach.After(dayEnd) {
		start = dayEnd.Add(-time.Duration(remainder) * time.Second)
	}
	end := start.Add(time.Duration(remainder) * time.Second)
	if _, err := insertClosedSessionTx(ctx, tx, teamID, activityID, start, end, "timesheet", 0); err != nil {
		return err
	}
	return nil
}

// ClearWeekRow empties every cell of one activity for the current actor in a
// single transaction. It repeats the cell write rules over the whole week —
// session owner lock, membership check, activity lock and invoice locks — so a
// row can never lose more than a cell write could, and a week with billed time
// stays untouched as a whole instead of half-cleared.
func (d *DB) ClearWeekRow(ctx context.Context, request appmodel.TimesheetRowClearRequest) error {
	teamID, activityID, week := request.TeamID, request.ActivityID, request.WeekStart
	if teamID <= 0 {
		return model.ErrForbidden
	}
	if activityID <= 0 || week.IsZero() {
		return fmt.Errorf("invalid timesheet row clear")
	}
	actor := actorID(ctx)
	if actor <= 0 {
		return model.ErrForbidden
	}
	weekStart := time.Date(week.Year(), week.Month(), week.Day(), 0, 0, 0, 0, week.Location())
	weekEnd := weekStart.AddDate(0, 0, 7)
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockApprovedSpan(ctx, tx, teamID, weekStart, weekEnd); err != nil {
		return err
	}
	if err := lockAutoClosed(ctx, tx, teamID, weekStart, d.currentTime()); err != nil {
		return err
	}
	if err := lockClosedPeriodSpan(ctx, tx, teamID, weekStart, weekEnd); err != nil {
		return err
	}
	if err := lockSessionOwner(ctx, tx, teamID); err != nil {
		return err
	}
	if err := requireTeamMembership(ctx, tx, teamID); err != nil {
		return err
	}
	// The week's sessions lock before the activity row for the same reason
	// as a cell write: the invoice builder locks sessions first, and the
	// opposite order is a deadlock between the two.
	weekSessions, err := tx.QueryContext(ctx,
		`SELECT id FROM sessions
		  WHERE activity_id = ? AND team_id = ? AND user_id = ?
		    AND end_at IS NOT NULL AND start_at >= ? AND start_at < ?
		  ORDER BY id FOR UPDATE`,
		activityID, teamID, actor, FormatTime(weekStart), FormatTime(weekEnd))
	if err != nil {
		return err
	}
	for weekSessions.Next() {
		var lockedID int64
		if err := weekSessions.Scan(&lockedID); err != nil {
			weekSessions.Close()
			return err
		}
	}
	if err := weekSessions.Err(); err != nil {
		weekSessions.Close()
		return err
	}
	if err := weekSessions.Close(); err != nil {
		return err
	}
	if err := lockActivityForSession(ctx, tx, teamID, activityID); err != nil {
		return err
	}
	if err := lockTimesheetInvoiceLocks(ctx, tx, teamID, activityID, weekStart, weekEnd); err != nil {
		return err
	}
	// Same scope as a cell write: only the actor's own closed sessions, so a
	// colleague's time on the same activity is never someone else's row to clear.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM sessions
		  WHERE activity_id = ? AND team_id = ? AND user_id = ?
		    AND end_at IS NOT NULL AND start_at >= ? AND start_at < ?`,
		activityID, teamID, actor, FormatTime(weekStart), FormatTime(weekEnd)); err != nil {
		return err
	}
	return tx.Commit()
}

// lockTimesheetInvoiceLocks refuses edits to tracked time that a sent or paid
// invoice already billed. [from, to) is a day range for a cell write and the
// whole week for a row clear.
func lockTimesheetInvoiceLocks(ctx context.Context, tx *Tx, teamID, activityID int64, from, to time.Time) error {
	query := `SELECT COALESCE(s.invoice_id, 0)
		FROM sessions s
		WHERE s.team_id = ? AND s.activity_id = ? AND s.end_at IS NOT NULL
		  AND s.start_at >= ? AND s.start_at < ? AND (? = 0 OR s.user_id = ?)
		ORDER BY s.id FOR UPDATE OF s`
	actor := actorID(ctx)
	rows, err := tx.QueryContext(ctx, query,
		teamID, activityID, FormatTime(from), FormatTime(to), actor, actor)
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
