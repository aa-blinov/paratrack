package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

var ErrInvalidPayrollQuery = errors.New("invalid payroll query")

type payrollQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func buildPayrollLines(ctx context.Context, queryer payrollQueryer, teamID int64, start, end time.Time, lockSessions bool, now time.Time) ([]PayrollLine, error) {
	if teamID <= 0 || start.IsZero() || !end.After(start) || now.IsZero() {
		return nil, ErrInvalidPayrollQuery
	}
	// members with a pay: hourly rates, or the fixed monthly salary (FIN-26)
	mrows, err := queryer.QueryContext(ctx,
		`SELECT m.user_id, COALESCE(m.hourly_pay_cents, 0), COALESCE(m.pay_kind, 'hourly'), COALESCE(m.monthly_pay_cents, 0), u.name, u.email, '' AS left_at
		 FROM memberships m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.team_id = ? AND (COALESCE(m.hourly_pay_cents, 0) > 0 OR (COALESCE(m.pay_kind, 'hourly') = 'monthly' AND COALESCE(m.monthly_pay_cents, 0) > 0))
		 UNION ALL
		 SELECT f.user_id, COALESCE(f.hourly_pay_cents, 0), COALESCE(f.pay_kind, 'hourly'), COALESCE(f.monthly_pay_cents, 0), u.name, u.email, f.left_at
		 FROM former_members f
		 JOIN users u ON u.id = f.user_id
		 WHERE f.team_id = ? AND (COALESCE(f.hourly_pay_cents, 0) > 0 OR (COALESCE(f.pay_kind, 'hourly') = 'monthly' AND COALESCE(f.monthly_pay_cents, 0) > 0))
		   AND NOT EXISTS (SELECT 1 FROM memberships m WHERE m.team_id = f.team_id AND m.user_id = f.user_id)`, teamID, teamID)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	type member struct {
		id     int64
		rate   int
		kind   string
		salary int
		name   string
		email  string
		left   string
	}
	var members []member
	for mrows.Next() {
		var m member
		if err := mrows.Scan(&m.id, &m.rate, &m.kind, &m.salary, &m.name, &m.email, &m.left); err != nil {
			mrows.Close()
			return nil, err
		}
		members = append(members, m)
	}
	mrows.Close()
	if err := mrows.Err(); err != nil {
		return nil, err
	}

	// tracked seconds per user in window
	secsByUser := map[int64]int{}
	// The branches give the plan a floor per index: the closed rows scan
	// (team_id, end_at) from the window's start instead of walking the whole
	// history. The union feeds an IN subquery because a locking clause cannot
	// sit on a statement whose top level is a set operation, while it can sit
	// on this plain single-table select.
	q := `
		SELECT COALESCE(s.user_id, 0), s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
		FROM sessions s
		WHERE s.id IN (
			SELECT id FROM (
				SELECT s.id FROM sessions s WHERE ` + windowedSessionsClosed(false) + `
				UNION ALL
				SELECT s.id FROM sessions s WHERE ` + windowedSessionsOpen() + `
			) w
		)`
	args := []any{teamID, FormatTime(start), FormatTime(end), teamID, FormatTime(end)}
	if lockSessions {
		// Id order keeps concurrent runs from locking the same rows in
		// opposite orders.
		q += ` ORDER BY s.id FOR UPDATE`
	}
	rows, err := queryer.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// Every session carries its author (user_id, stamped on every create
	// path; older rows were given to the workspace owner at startup), so
	// each member is paid for their own time only.
	for rows.Next() {
		var (
			owner                      int64
			startAt, endAt, lastResume sql.NullString
			accum, paused              int
		)
		if err := rows.Scan(&owner, &startAt, &endAt, &accum, &paused, &lastResume); err != nil {
			return nil, err
		}
		st, err := ScanTime(startAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse payroll session start: %w", err)
		}
		sess := model.Session{StartAt: st, AccumulatedSeconds: accum, Paused: paused == 1}
		if endAt.Valid {
			t, err := ScanTime(endAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse payroll session end: %w", err)
			}
			sess.EndAt = &t
		}
		if lastResume.Valid {
			t, err := ScanTime(lastResume.String)
			if err != nil {
				return nil, fmt.Errorf("parse payroll session resume time: %w", err)
			}
			sess.LastResumeAt = &t
		}
		sec := sess.TrackedSecondsInWindow(start, end, now)
		if sec <= 0 {
			continue
		}
		secsByUser[owner], err = money.AddInt(secsByUser[owner], sec)
		if err != nil {
			return nil, fmt.Errorf("sum payroll time for member %d: %w", owner, err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Months the period covers, for the salary line (FIN-26): a salaried
	// member is paid their salary per calendar month the run touches, in
	// whole months — the studio's monthly runs pay one each.
	salary, err := salaryForMonths(start, end)
	if err != nil {
		return nil, err
	}

	lines := make([]PayrollLine, 0, len(members))
	for _, m := range members {
		label := m.name
		if label == "" {
			label = m.email
		}
		if m.kind == "monthly" {
			// The salary prices the month, not the hours: tracked time stays
			// unpriced on top of a fixed pay.
			if m.salary <= 0 || salary <= 0 {
				continue
			}
			months := salary
			if m.left != "" {
				// A former member prices only the months they were present
				// for: the departure row keeps the pay they left with, and a
				// month entirely after the departure is not theirs.
				leftAt, err := ParseTime(m.left)
				if err != nil {
					return nil, fmt.Errorf("parse former member departure: %w", err)
				}
				months = monthsPresent(start, end, leftAt)
				if months <= 0 {
					continue
				}
			}
			// The only money multiplication in the payroll path: the guard
			// lives with it, like money.AddInt guards the additions.
			if int64(months) > ((1<<63)-1)/int64(m.salary) {
				return nil, fmt.Errorf("salary for member %d overflows cents", m.id)
			}
			lines = append(lines, PayrollLine{
				UserID: m.id, Label: label,
				Seconds: 0, RateCents: 0, AmountCents: m.salary * months,
			})
			continue
		}
		sec := secsByUser[m.id]
		if HoursHundredths(sec) == 0 {
			continue
		}
		amount, err := PriceCents(sec, m.rate)
		if err != nil {
			return nil, fmt.Errorf("price payroll member %d: %w", m.id, err)
		}
		lines = append(lines, PayrollLine{
			UserID: m.id, Label: label,
			Seconds: sec, RateCents: m.rate, AmountCents: amount,
		})
	}
	return lines, nil
}

// salaryForMonths counts the calendar months the period overlaps: one run
// normally covers a month, a two-month run pays two salaries.
func salaryForMonths(start, end time.Time) (int, error) {
	if !end.After(start) {
		return 0, ErrInvalidPayrollQuery
	}
	first := start
	last := end.Add(-time.Nanosecond)
	months := (last.Year()-first.Year())*12 + int(last.Month()) - int(first.Month()) + 1
	if months <= 0 {
		return 0, ErrInvalidPayrollQuery
	}
	return months, nil
}

// monthsPresent counts the calendar months of the period that begin at or
// before the departure instant. A former member's salary prices the months
// they were a member for at least in part; a run that starts after the
// departure prices none.
func monthsPresent(start, end, leftAt time.Time) int {
	first := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location())
	last := end.Add(-time.Nanosecond)
	lastMonth := time.Date(last.Year(), last.Month(), 1, 0, 0, 0, 0, last.Location())
	if leftMonth := time.Date(leftAt.Year(), leftAt.Month(), 1, 0, 0, 0, 0, leftAt.Location()); lastMonth.After(leftMonth) {
		lastMonth = leftMonth
	}
	if lastMonth.Before(first) {
		return 0
	}
	return (lastMonth.Year()-first.Year())*12 + int(lastMonth.Month()) - int(first.Month()) + 1
}
