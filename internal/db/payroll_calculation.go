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
		`SELECT m.user_id, COALESCE(m.hourly_pay_cents, 0), COALESCE(m.pay_kind, 'hourly'), COALESCE(m.monthly_pay_cents, 0), u.name, u.email, '' AS left_at, COALESCE(NULLIF(m.work_days, ''), '12345'), COALESCE(m.employed_from, '')
		 FROM memberships m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.team_id = ? AND (COALESCE(m.hourly_pay_cents, 0) > 0 OR (COALESCE(m.pay_kind, 'hourly') = 'monthly' AND COALESCE(m.monthly_pay_cents, 0) > 0))
		 UNION ALL
		 SELECT f.user_id, COALESCE(f.hourly_pay_cents, 0), COALESCE(f.pay_kind, 'hourly'), COALESCE(f.monthly_pay_cents, 0), u.name, u.email, f.left_at, COALESCE(NULLIF(f.work_days, ''), '12345'), COALESCE(f.employed_from, '')
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
		days   string
		joined string
	}
	var members []member
	for mrows.Next() {
		var m member
		if err := mrows.Scan(&m.id, &m.rate, &m.kind, &m.salary, &m.name, &m.email, &m.left, &m.days, &m.joined); err != nil {
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

	// The salary follows the month's working days (FIN-26): a month a member
	// was away for part of pays the share of the days to be worked that they
	// were not away, by their own week, the holidays and the transfers.
	calendar, err := loadSalaryCalendar(ctx, queryer, teamID, start, end)
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
			// The only money multiplication in the payroll path: the guard
			// lives with it, like money.AddInt guards the additions.
			if int64(salary) > ((1<<63)-1)/int64(m.salary) {
				return nil, fmt.Errorf("salary for member %d overflows cents", m.id)
			}
			// The days before joining and after leaving are not theirs: a
			// member who joined or left in the month is paid their share of
			// it, by the week they worked; a month wholly outside is not paid.
			var joinedAt, leftAt time.Time
			if m.joined != "" {
				if joinedAt, err = time.ParseInLocation("2006-01-02", m.joined, start.Location()); err != nil {
					return nil, fmt.Errorf("parse member employment start: %w", err)
				}
			}
			if m.left != "" {
				if leftAt, err = ParseTime(m.left); err != nil {
					return nil, fmt.Errorf("parse former member departure: %w", err)
				}
			}
			amount, worked, norm := calendar.salary(m.id, m.days, m.salary, start, end, joinedAt, leftAt)
			if worked <= 0 || amount <= 0 {
				continue
			}
			if worked < norm {
				label = fmt.Sprintf("%s (%d из %d дн.)", label, worked, norm)
			}
			lines = append(lines, PayrollLine{
				UserID: m.id, Label: label,
				Seconds: 0, RateCents: 0, AmountCents: amount,
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

// salaryCalendar holds the days that decide a salary over a run: the
// workspace's holidays and transfers, and who was away when.
type salaryCalendar struct {
	off, work map[string]bool
	away      map[int64]map[string]bool
}

func loadSalaryCalendar(ctx context.Context, q payrollQueryer, teamID int64, start, end time.Time) (salaryCalendar, error) {
	cal := salaryCalendar{off: map[string]bool{}, work: map[string]bool{}, away: map[int64]map[string]bool{}}
	first := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location()).Format("2006-01-02")
	last := end.Add(-time.Nanosecond)
	until := time.Date(last.Year(), last.Month(), 1, 0, 0, 0, 0, last.Location()).AddDate(0, 1, 0).Format("2006-01-02")
	rows, err := q.QueryContext(ctx, `SELECT day, kind FROM team_holidays WHERE team_id = ? AND day >= ? AND day < ?`, teamID, first, until)
	if err != nil {
		return cal, err
	}
	for rows.Next() {
		var day, kind string
		if err := rows.Scan(&day, &kind); err != nil {
			rows.Close()
			return cal, err
		}
		switch kind {
		case "off":
			cal.off[day] = true
		case "work":
			cal.work[day] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return cal, err
	}
	rows, err = q.QueryContext(ctx, `SELECT user_id, day FROM member_absences WHERE team_id = ? AND day >= ? AND day < ?`, teamID, first, until)
	if err != nil {
		return cal, err
	}
	for rows.Next() {
		var userID int64
		var day string
		if err := rows.Scan(&userID, &day); err != nil {
			rows.Close()
			return cal, err
		}
		if cal.away[userID] == nil {
			cal.away[userID] = map[string]bool{}
		}
		cal.away[userID][day] = true
	}
	rows.Close()
	return cal, rows.Err()
}

// salary prices a member's salary over the calendar months a run touches:
// each month pays salary × days worked ÷ days to be worked, rounded to the
// cent. It answers the amount and the days, summed over the months.
func (c salaryCalendar) salary(userID int64, workDays string, monthly int, start, end, joinedAt, leftAt time.Time) (amount, worked, norm int) {
	loc := start.Location()
	firstDay, lastDay := "", ""
	if !joinedAt.IsZero() {
		firstDay = joinedAt.In(loc).Format("2006-01-02")
	}
	if !leftAt.IsZero() {
		lastDay = leftAt.In(loc).Format("2006-01-02")
	}
	month := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, start.Location())
	last := end.Add(-time.Nanosecond)
	for !month.After(last) {
		next := month.AddDate(0, 1, 0)
		monthNorm, monthWorked := 0, 0
		for day := month; day.Before(next); day = day.AddDate(0, 0, 1) {
			key := day.Format("2006-01-02")
			if c.off[key] || !(model.WorksOn(workDays, day.Weekday()) || c.work[key]) {
				continue
			}
			monthNorm++
			member := (firstDay == "" || key >= firstDay) && (lastDay == "" || key <= lastDay)
			if member && !c.away[userID][key] {
				monthWorked++
			}
		}
		if monthNorm > 0 {
			amount += int((int64(monthly)*int64(monthWorked) + int64(monthNorm)/2) / int64(monthNorm))
		}
		worked += monthWorked
		norm += monthNorm
		month = next
	}
	return amount, worked, norm
}
