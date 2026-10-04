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
	// members with pay > 0
	mrows, err := queryer.QueryContext(ctx,
		`SELECT m.user_id, COALESCE(m.hourly_pay_cents, 0), u.name, u.email
		 FROM memberships m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.team_id = ? AND COALESCE(m.hourly_pay_cents, 0) > 0
		 UNION ALL
		 SELECT f.user_id, COALESCE(f.hourly_pay_cents, 0), u.name, u.email
		 FROM former_members f
		 JOIN users u ON u.id = f.user_id
		 WHERE f.team_id = ? AND COALESCE(f.hourly_pay_cents, 0) > 0
		   AND NOT EXISTS (SELECT 1 FROM memberships m WHERE m.team_id = f.team_id AND m.user_id = f.user_id)`, teamID, teamID)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	type member struct {
		id    int64
		rate  int
		name  string
		email string
	}
	var members []member
	for mrows.Next() {
		var m member
		if err := mrows.Scan(&m.id, &m.rate, &m.name, &m.email); err != nil {
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
	q := `
		SELECT COALESCE(s.user_id, 0), s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
		FROM sessions s
		WHERE s.start_at < ?
		  AND (s.end_at IS NULL OR s.end_at >= ?)`
	args := []any{FormatTime(end), FormatTime(start)}
	q += ` AND s.team_id = ?`
	args = append(args, teamID)
	if lockSessions {
		q += ` ORDER BY s.id FOR UPDATE OF s`
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

	lines := make([]PayrollLine, 0, len(members))
	for _, m := range members {
		sec := secsByUser[m.id]
		if HoursHundredths(sec) == 0 {
			continue
		}
		amount, err := PriceCents(sec, m.rate)
		if err != nil {
			return nil, fmt.Errorf("price payroll member %d: %w", m.id, err)
		}
		label := m.name
		if label == "" {
			label = m.email
		}
		lines = append(lines, PayrollLine{
			UserID: m.id, Label: label,
			Seconds: sec, RateCents: m.rate, AmountCents: amount,
		})
	}
	return lines, nil
}
