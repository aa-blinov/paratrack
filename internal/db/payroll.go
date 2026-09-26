package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------
// Wave 6: payroll + resource scheduling
// ---------------------------------------------------------------------------

// PayrollRun is a pay run for the team over a period.
type PayrollRun struct {
	ID          int64     `json:"id"`
	TeamID      int64     `json:"team_id"`
	Number      string    `json:"number"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Status      string    `json:"status"` // draft | paid
	Notes       string    `json:"notes"`
	CreatedAt   time.Time `json:"created_at"`
}

// PayrollLine is one person's row on a pay run.
type PayrollLine struct {
	ID          int64  `json:"id"`
	RunID       int64  `json:"run_id"`
	UserID      int64  `json:"user_id"`
	Label       string `json:"label"`
	Seconds     int    `json:"seconds"`
	RateCents   int    `json:"rate_cents"`
	AmountCents int    `json:"amount_cents"`
}

// ScheduleEntry is planned time: person × project × day.
type ScheduleEntry struct {
	ID        int64  `json:"id"`
	TeamID    int64  `json:"team_id"`
	UserID    int64  `json:"user_id"`
	ProjectID int64  `json:"project_id"`
	Day       string `json:"day"` // YYYY-MM-DD
	Minutes   int    `json:"minutes"`
	Note      string `json:"note"`
}

// SetMemberPay updates a membership's pay rate and daily capacity.
// payCents = hourly pay; capacityMinutes = planned minutes/day (0 = 480).
func (d *DB) SetMemberPay(ctx context.Context, teamID, userID int64, payCents, capacityMinutes *int) error {
	q := `UPDATE memberships SET `
	args := []any{}
	sets := []string{}
	if payCents != nil {
		if *payCents < 0 {
			return fmt.Errorf("pay must be >= 0")
		}
		sets = append(sets, "hourly_pay_cents = ?")
		args = append(args, *payCents)
	}
	if capacityMinutes != nil {
		if *capacityMinutes < 0 {
			return fmt.Errorf("capacity must be >= 0")
		}
		sets = append(sets, "capacity_minutes = ?")
		args = append(args, *capacityMinutes)
	}
	if len(sets) == 0 {
		return nil
	}
	q += strings.Join(sets, ", ") + " WHERE team_id = ? AND user_id = ?"
	args = append(args, teamID, userID)
	res, err := d.sql.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MemberPay returns the pay rate + capacity for a user in a team.
func (d *DB) MemberPay(ctx context.Context, teamID, userID int64) (payCents, capacityMinutes int, err error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT COALESCE(hourly_pay_cents, 0), COALESCE(capacity_minutes, 0)
		 FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, userID)
	err = row.Scan(&payCents, &capacityMinutes)
	return
}

// BuildPayrollLines aggregates each member's tracked time in [start,end)
// and prices it at their hourly pay. Members with 0 pay are skipped
// (they are not on payroll).
func (d *DB) BuildPayrollLines(ctx context.Context, teamID int64, start, end time.Time) ([]PayrollLine, error) {
	// members with pay > 0
	mrows, err := d.sql.QueryContext(ctx,
		`SELECT m.user_id, COALESCE(m.hourly_pay_cents, 0), u.name, u.email
		 FROM memberships m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.team_id = ? AND COALESCE(m.hourly_pay_cents, 0) > 0`, teamID)
	if err != nil {
		return nil, err
	}
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
	now := time.Now()
	secsByUser := map[int64]int{}
	q := `
		SELECT COALESCE(s.user_id, 0), s.start_at, s.end_at, s.accumulated_seconds, s.paused, s.last_resume_at
		FROM sessions s
		WHERE s.start_at < ?
		  AND (s.end_at IS NULL OR s.end_at >= ?)`
	args := []any{FormatTime(end), FormatTime(start)}
	if teamID > 0 {
		q += ` AND s.team_id = ?`
		args = append(args, teamID)
	}
	rows, err := d.sql.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// We attribute sessions to the user via a lookup: sessions have no
	// user_id (they belong to the team). For payroll we use the team
	// member who has a pay rate and split evenly is wrong — so we add
	// user_id to sessions? For Wave 6 we attribute ALL team tracked time
	// to each paid member only if they are the sole paid member; better:
	// require sessions.user_id. columnMigrations adds it below and
	// handleStart stamps it. For rows without a user (legacy / CLI),
	// they land on the team's first paid member.
	for rows.Next() {
		var (
			owner                               int64
			startAt, endAt, lastResume          sql.NullString
			accum, paused                       int
		)
		if err := rows.Scan(&owner, &startAt, &endAt, &accum, &paused, &lastResume); err != nil {
			return nil, err
		}
		st, err := ScanTime(startAt.String)
		if err != nil {
			continue
		}
		sess := model.Session{StartAt: st, AccumulatedSeconds: accum, Paused: paused == 1}
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
		sec := sess.TrackedSecondsInWindow(start, end, now)
		if sec <= 0 {
			continue
		}
		// Legacy rows without user_id land on the first paid member;
		// handleStart stamps user_id going forward.
		uid := owner
		if uid == 0 && len(members) > 0 {
			uid = members[0].id
		}
		secsByUser[uid] += sec
	}

	lines := make([]PayrollLine, 0, len(members))
	for _, m := range members {
		sec := secsByUser[m.id]
		if HoursHundredths(sec) == 0 {
			continue
		}
		amount := PriceCents(sec, m.rate)
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

// NextPayrollNumber returns "PAY-YYYY-NNN".
func (d *DB) NextPayrollNumber(ctx context.Context, teamID int64) (string, error) {
	year := time.Now().Year()
	prefix := fmt.Sprintf("PAY-%d-", year)
	var maxNum int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(CAST(SUBSTR(number, LENGTH(?) + 1) AS INTEGER)), 0)
		 FROM payroll_runs WHERE team_id = ? AND number LIKE ?`,
		prefix, teamID, prefix+"%").Scan(&maxNum)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%03d", prefix, maxNum+1), nil
}

// CreatePayrollRun stores a run with its lines.
func (d *DB) CreatePayrollRun(ctx context.Context, teamID int64, number, notes string,
	start, end time.Time, lines []PayrollLine) (PayrollRun, error) {
	if number == "" {
		return PayrollRun{}, fmt.Errorf("number is required")
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return PayrollRun{}, err
	}
	defer func() { _ = tx.Rollback() }()
	now := FormatTime(time.Now().UTC())
	var runID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO payroll_runs (team_id, number, period_start, period_end, status, notes, created_at)
		 VALUES (?, ?, ?, ?, 'draft', ?, ?) RETURNING id`,
		teamID, number, FormatTime(start), FormatTime(end), notes, now).Scan(&runID)
	if err != nil {
		if isUniqueViolation(err) {
			return PayrollRun{}, ErrDuplicate
		}
		return PayrollRun{}, err
	}
	for _, l := range lines {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			runID, l.UserID, l.Label, l.Seconds, l.RateCents, l.AmountCents); err != nil {
			return PayrollRun{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return PayrollRun{}, err
	}
	return d.GetPayrollRun(ctx, teamID, runID)
}

// ListPayrollRuns returns the team's pay runs, newest first.
func (d *DB) ListPayrollRuns(ctx context.Context, teamID int64) ([]PayrollRun, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, number, period_start, period_end, status, notes, created_at
		 FROM payroll_runs WHERE team_id = ? ORDER BY created_at DESC`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayrollRun
	for rows.Next() {
		r, err := scanPayrollRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetPayrollRun fetches one run.
func (d *DB) GetPayrollRun(ctx context.Context, teamID, id int64) (PayrollRun, error) {
	row := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, number, period_start, period_end, status, notes, created_at
		 FROM payroll_runs WHERE id = ? AND team_id = ?`, id, teamID)
	r, err := scanPayrollRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return PayrollRun{}, ErrNotFound
	}
	return r, err
}

// ListPayrollLines returns the run's lines.
func (d *DB) ListPayrollLines(ctx context.Context, runID int64) ([]PayrollLine, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, run_id, user_id, label, seconds, rate_cents, amount_cents
		 FROM payroll_lines WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayrollLine
	for rows.Next() {
		var l PayrollLine
		if err := rows.Scan(&l.ID, &l.RunID, &l.UserID, &l.Label, &l.Seconds, &l.RateCents, &l.AmountCents); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// MarkPayrollPaid flags the run paid.
func (d *DB) MarkPayrollPaid(ctx context.Context, teamID, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`UPDATE payroll_runs SET status = 'paid' WHERE id = ? AND team_id = ?`, id, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePayrollRun removes a run and its lines.
func (d *DB) DeletePayrollRun(ctx context.Context, teamID, id int64) error {
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM payroll_runs WHERE id = ? AND team_id = ?`, id, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanPayrollRun(r interface{ Scan(...any) error }) (PayrollRun, error) {
	var (
		run       PayrollRun
		start, end, ct string
	)
	if err := r.Scan(&run.ID, &run.TeamID, &run.Number, &start, &end, &run.Status, &run.Notes, &ct); err != nil {
		return PayrollRun{}, err
	}
	run.PeriodStart, _ = ScanTime(start)
	run.PeriodEnd, _ = ScanTime(end)
	run.CreatedAt, _ = ScanTime(ct)
	return run, nil
}

// ---------------------------------------------------------------------------
// Resource scheduling
// ---------------------------------------------------------------------------

// UpsertScheduleEntry sets planned minutes for (user, project, day).
func (d *DB) UpsertScheduleEntry(ctx context.Context, teamID, userID, projectID int64, day string, minutes int, note string) error {
	if minutes < 0 || minutes > 24*60 {
		return fmt.Errorf("minutes must be 0..1440")
	}
	now := FormatTime(time.Now().UTC())
	if minutes == 0 {
		_, err := d.sql.ExecContext(ctx,
			`DELETE FROM schedule_entries WHERE team_id = ? AND user_id = ? AND project_id = ? AND day = ?`,
			teamID, userID, projectID, day)
		return err
	}
	_, err := d.sql.ExecContext(ctx, `
		INSERT INTO schedule_entries (team_id, user_id, project_id, day, minutes, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (team_id, user_id, project_id, day) DO UPDATE SET
		  minutes = excluded.minutes,
		  note = excluded.note`,
		teamID, userID, projectID, day, minutes, note, now)
	return err
}

// ScheduleRow is one person's planned week.
type ScheduleRow struct {
	UserID   int64
	UserName string
	Capacity int // planned minutes/day (0 → 480)
	// Minutes indexed Mon..Sun
	Minutes [7]int
	Total   int
}

// ListSchedule returns the plan for a week plus a project breakdown.
// weekStart must be a Monday.
func (d *DB) ListSchedule(ctx context.Context, teamID int64, weekStart time.Time) ([]ScheduleRow, map[int64]string, error) {
	weekEnd := weekStart.AddDate(0, 0, 7)
	dayStr := func(t time.Time) string { return t.Format("2006-01-02") }

	// users in the team
	urows, err := d.sql.QueryContext(ctx,
		`SELECT u.id, u.name, u.email, COALESCE(m.capacity_minutes, 0)
		 FROM memberships m JOIN users u ON u.id = m.user_id
		 WHERE m.team_id = ? ORDER BY u.name COLLATE NOCASE`, teamID)
	if err != nil {
		return nil, nil, err
	}
	type userRec struct {
		id   int64
		name string
		cap  int
	}
	var users []userRec
	for urows.Next() {
		var u userRec
		var email string
		if err := urows.Scan(&u.id, &u.name, &email, &u.cap); err != nil {
			urows.Close()
			return nil, nil, err
		}
		if u.name == "" {
			u.name = email
		}
		if u.cap <= 0 {
			u.cap = 480
		}
		users = append(users, u)
	}
	urows.Close()

	// project names
	pnames := map[int64]string{}
	prows, _ := d.sql.QueryContext(ctx, `SELECT id, name FROM projects WHERE team_id = ?`, teamID)
	if prows != nil {
		for prows.Next() {
			var id int64
			var name string
			if prows.Scan(&id, &name) == nil {
				pnames[id] = name
			}
		}
		prows.Close()
	}

	// entries in window
	q := `SELECT user_id, project_id, day, minutes FROM schedule_entries
	      WHERE team_id = ? AND day >= ? AND day < ?`
	erows, err := d.sql.QueryContext(ctx, q, teamID, dayStr(weekStart), dayStr(weekEnd))
	if err != nil {
		return nil, nil, err
	}
	type key struct{ uid int64; day string }
	entry := map[key]int{}
	for erows.Next() {
		var uid, pid, mins int64
		var day string
		var m int
		if err := erows.Scan(&uid, &pid, &day, &m); err == nil {
			entry[key{uid, day}] += m
		}
		_ = mins
	}
	erows.Close()

	rows := make([]ScheduleRow, 0, len(users))
	for _, u := range users {
		row := ScheduleRow{UserID: u.id, UserName: u.name, Capacity: u.cap}
		for i := 0; i < 7; i++ {
			day := dayStr(weekStart.AddDate(0, 0, i))
			row.Minutes[i] = entry[key{u.id, day}]
			row.Total += row.Minutes[i]
		}
		rows = append(rows, row)
	}
	return rows, pnames, nil
}
