package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// ProjectsDueBetween lists projects whose deadline falls in [from, to] and
// whose reminder for that deadline has not gone out yet, across all teams.
func (d *DB) ProjectsDueBetween(ctx context.Context, from, to string) ([]appmodel.DueProject, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT team_id, id, name, slug, deadline, COALESCE(lead_user_id, 0)
		   FROM projects
		  WHERE archived = 0 AND deadline IS NOT NULL AND deadline >= ? AND deadline <= ?
		    AND (deadline_reminded IS NULL OR deadline_reminded <> deadline)
		  ORDER BY deadline, id`, from, to)
	if err != nil {
		return nil, fmt.Errorf("list due projects: %w", err)
	}
	defer rows.Close()
	var due []appmodel.DueProject
	for rows.Next() {
		var p appmodel.DueProject
		if err := rows.Scan(&p.TeamID, &p.ProjectID, &p.Name, &p.Slug, &p.Deadline, &p.LeadUserID); err != nil {
			return nil, err
		}
		due = append(due, p)
	}
	return due, rows.Err()
}

// MarkDeadlineReminded records that the reminder for this deadline went out.
func (d *DB) MarkDeadlineReminded(ctx context.Context, request appmodel.DeadlineReminderRequest) error {
	_, err := d.sql.ExecContext(ctx,
		`UPDATE projects SET deadline_reminded = ? WHERE id = ? AND team_id = ?`, request.Deadline, request.ProjectID, request.TeamID)
	return err
}

// ManagerIDs lists the owner and the administrators of a workspace.
func (d *DB) ManagerIDs(ctx context.Context, teamID int64) ([]int64, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT user_id FROM memberships WHERE team_id = ? AND role IN ('owner', 'admin')
		 UNION
		 SELECT owner_id FROM teams WHERE id = ?`, teamID, teamID)
	if err != nil {
		return nil, fmt.Errorf("list managers: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// TeamIDs lists every workspace, for the periodic checks.
func (d *DB) TeamIDs(ctx context.Context) ([]int64, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT id FROM teams ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ClaimOverloadNotice records that a person's overload this week was reported.
// It returns true only for the first claim, so the notice goes out once.
// The claim is stored before the notice is sent: a failed send is then not
// retried, which is the safer side of a nudge that must not repeat.
func (d *DB) ClaimOverloadNotice(ctx context.Context, request appmodel.OverloadNoticeRequest) (bool, error) {
	result, err := d.sql.ExecContext(ctx,
		`INSERT INTO schedule_overload_reminders (team_id, user_id, week, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (team_id, user_id, week) DO NOTHING`,
		request.TeamID, request.UserID, request.Week, FormatTime(d.currentTime().UTC()))
	if err != nil {
		return false, fmt.Errorf("claim overload notice: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// DayFillCandidates lists the members of a workspace who started no session in the
// window. A running timer started inside the window counts as logged time.
func (d *DB) DayFillCandidates(ctx context.Context, query appmodel.DayFillCandidatesQuery) ([]int64, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT m.user_id FROM memberships m
		  WHERE m.team_id = ?
		    AND NOT EXISTS (
		      SELECT 1 FROM sessions s
		       WHERE s.team_id = m.team_id AND s.user_id = m.user_id
		         AND s.start_at >= ? AND s.start_at < ?)
		    AND NOT EXISTS (
		      SELECT 1 FROM member_absences a
		       WHERE a.team_id = m.team_id AND a.user_id = m.user_id AND a.day = ?)
		    AND NOT EXISTS (
		      SELECT 1 FROM team_holidays h WHERE h.team_id = m.team_id AND h.day = ?)
		  ORDER BY m.user_id`,
		query.TeamID, FormatTime(query.From), FormatTime(query.To), query.Day, query.Day)
	if err != nil {
		return nil, fmt.Errorf("list members without time: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ClaimDayFillNotice records that a person was asked to fill a workspace day. It
// returns true only for the first claim, so the notice goes out once per day.
func (d *DB) ClaimDayFillNotice(ctx context.Context, request appmodel.DayFillNoticeRequest) (bool, error) {
	result, err := d.sql.ExecContext(ctx,
		`INSERT INTO day_fill_reminders (team_id, user_id, day, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (team_id, user_id, day) DO NOTHING`,
		request.TeamID, request.UserID, request.Day, FormatTime(d.currentTime().UTC()))
	if err != nil {
		return false, fmt.Errorf("claim day fill notice: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// TeamLocation is the workspace's time zone. A blank or unknown zone is UTC, so a
// bad setting in one workspace cannot stop the reminders of the others.
func (d *DB) TeamLocation(ctx context.Context, teamID int64) (*time.Location, error) {
	var zone string
	if err := d.sql.QueryRowContext(ctx, `SELECT timezone FROM teams WHERE id = ?`, teamID).Scan(&zone); err != nil {
		return nil, fmt.Errorf("read workspace time zone: %w", err)
	}
	if zone == "" {
		return time.UTC, nil
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.UTC, nil
	}
	return loc, nil
}

// UserTimeZones reads each member's hand-set zone from their preferences row
// (the "tz" key, empty when they never chose one). The reminders' night gate
// (UX-33) uses it for workspaces that left their own zone blank: a member
// whose zone says night keeps their push until morning. An invalid zone
// resolves like an absent one — its owner reads the workspace fallback.
func (d *DB) UserTimeZones(ctx context.Context, userIDs []int64) (map[int64]string, error) {
	zones := make(map[int64]string, len(userIDs))
	if len(userIDs) == 0 {
		return zones, nil
	}
	marks := make([]string, len(userIDs))
	args := make([]any, len(userIDs))
	for i, id := range userIDs {
		marks[i] = "?"
		args[i] = id
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, prefs FROM users WHERE id IN (`+strings.Join(marks, ", ")+`)`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("read member time zones: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		zones[id] = prefsZone(raw)
	}
	return zones, rows.Err()
}

// prefsZone pulls the hand-set zone out of a preferences JSON row. A row that
// never existed, is empty or does not parse answers empty, the same as "never
// chose": one broken row must not decide whether its owner wakes at night.
func prefsZone(raw string) string {
	if raw == "" {
		return ""
	}
	var prefs struct {
		TZ string `json:"tz"`
	}
	if json.Unmarshal([]byte(raw), &prefs) != nil {
		return ""
	}
	return prefs.TZ
}

// RunningSessions lists the timers running now, each with its owner's
// long-timer threshold in hours (4 unless they chose otherwise, 0 is off).
func (d *DB) RunningSessions(ctx context.Context) ([]appmodel.RunningSession, error) {
	var out []appmodel.RunningSession
	err := queryRows(ctx, d.sql, "running sessions",
		`SELECT s.team_id, s.user_id, s.id, a.name, s.start_at, COALESCE(s.last_resume_at, ''), COALESCE(s.accumulated_seconds, 0),
		        COALESCE((NULLIF(u.prefs, '')::jsonb ->> 'long_timer_hours')::int, 4)
		   FROM sessions s
		   JOIN activities a ON a.id = s.activity_id
		   JOIN users u ON u.id = s.user_id
		  WHERE s.end_at IS NULL AND s.paused = 0
		    AND NOT EXISTS (SELECT 1 FROM long_timer_reminders r WHERE r.session_id = s.id)
		  ORDER BY s.id`,
		func(rows *sql.Rows) error {
			var r appmodel.RunningSession
			var start, resume string
			if err := rows.Scan(&r.TeamID, &r.UserID, &r.SessionID, &r.Activity, &start, &resume, &r.Accumulated, &r.ThresholdHours); err != nil {
				return err
			}
			var err error
			if r.StartAt, err = ScanTime(start); err != nil {
				return err
			}
			if resume != "" {
				at, err := ScanTime(resume)
				if err != nil {
					return err
				}
				r.LastResumeAt = &at
			}
			out = append(out, r)
			return nil
		})
	return out, err
}

// ClaimLongTimerNotice records the one long-timer reminder a session gets.
func (d *DB) ClaimLongTimerNotice(ctx context.Context, request appmodel.LongTimerNoticeRequest) (bool, error) {
	sessionID := request.SessionID
	result, err := d.sql.ExecContext(ctx,
		`INSERT INTO long_timer_reminders (session_id, created_at) VALUES (?, ?) ON CONFLICT (session_id) DO NOTHING`,
		sessionID, FormatTime(d.currentTime().UTC()))
	if err != nil {
		return false, fmt.Errorf("claim long timer notice: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// BudgetProjects lists the live projects with an estimate or a money budget,
// with the time tracked on them so far and their rate, for the budget warnings.
func (d *DB) BudgetProjects(ctx context.Context) ([]appmodel.BudgetProject, error) {
	var out []appmodel.BudgetProject
	err := queryRows(ctx, d.sql, "budget projects",
		`SELECT p.team_id, p.id, p.name, p.slug, COALESCE(p.lead_user_id, 0),
		        COALESCE(p.estimate_minutes, 0), COALESCE(p.budget_cents, 0), COALESCE(p.billable_rate_cents, 0),
		        COALESCE((SELECT SUM(s.accumulated_seconds) FROM sessions s JOIN activities a ON a.id = s.activity_id
		                   WHERE a.project_id = p.id AND s.end_at IS NOT NULL), 0)
		   FROM projects p
		  WHERE p.archived = 0 AND (COALESCE(p.estimate_minutes, 0) > 0 OR COALESCE(p.budget_cents, 0) > 0)
		  ORDER BY p.id`,
		func(rows *sql.Rows) error {
			var p appmodel.BudgetProject
			if err := rows.Scan(&p.TeamID, &p.ProjectID, &p.Name, &p.Slug, &p.LeadUserID, &p.EstimateMinutes, &p.BudgetCents, &p.RateCents, &p.TrackedSeconds); err != nil {
				return err
			}
			out = append(out, p)
			return nil
		})
	return out, err
}

// ClaimBudgetNotice records one warning level of a project, once.
func (d *DB) ClaimBudgetNotice(ctx context.Context, request appmodel.BudgetNoticeRequest) (bool, error) {
	result, err := d.sql.ExecContext(ctx,
		`INSERT INTO project_budget_reminders (project_id, level, created_at) VALUES (?, ?, ?) ON CONFLICT (project_id, level) DO NOTHING`,
		request.ProjectID, request.Level, FormatTime(d.currentTime().UTC()))
	if err != nil {
		return false, fmt.Errorf("claim budget notice: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
