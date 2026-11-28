package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// MarkPayrollPaidWithRecipients commits the paid transition and reads the
// notification recipients from the same locked run snapshot.
func (d *DB) MarkPayrollPaidWithRecipients(ctx context.Context, request appmodel.PayrollMutationRequest) (bool, []int64, error) {
	teamID, id, callerID := request.TeamID, request.RunID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return false, nil, err
	}
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM payroll_runs WHERE id = ? AND team_id = ? FOR UPDATE`, id, teamID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil, ErrNotFound
		}
		return false, nil, err
	}
	changed := status != "paid"
	if changed {
		if _, err := tx.ExecContext(ctx, `UPDATE payroll_runs SET status = 'paid' WHERE id = ? AND team_id = ?`, id, teamID); err != nil {
			return false, nil, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT user_id FROM payroll_lines WHERE run_id = ? ORDER BY user_id`, id)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	var recipients []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return false, nil, err
		}
		recipients = append(recipients, userID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, nil, err
	}
	if err := rows.Close(); err != nil {
		return false, nil, err
	}
	if err := tx.Commit(); err != nil {
		return false, nil, err
	}
	return changed, recipients, nil
}

// DeletePayrollDraft removes only an unpaid run and its lines. Status is part
// of the predicate so a concurrent payment cannot race the deletion.
func (d *DB) DeletePayrollDraft(ctx context.Context, request appmodel.PayrollMutationRequest) error {
	teamID, id, callerID := request.TeamID, request.RunID, request.CallerID
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM payroll_runs WHERE id = ? AND team_id = ? AND status = 'draft'`, id, teamID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM payroll_runs WHERE id = ? AND team_id = ?)`, id, teamID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return model.ErrForbidden
	}
	return tx.Commit()
}

// AddPayrollAdjustment appends a one-off bonus or deduction line to a draft run.
func (d *DB) AddPayrollAdjustment(ctx context.Context, request appmodel.PayrollAdjustmentRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM payroll_runs WHERE id = ? AND team_id = ? FOR UPDATE`, request.RunID, request.TeamID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status != "draft" {
		return model.ErrPayrollNotDraft
	}
	var userName string
	if err := tx.QueryRowContext(ctx,
		`SELECT u.name FROM memberships m JOIN users u ON u.id = m.user_id WHERE m.team_id = ? AND m.user_id = ?`,
		request.TeamID, request.UserID).Scan(&userName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	label := strings.TrimSpace(userName) + " — " + strings.TrimSpace(request.Label)
	amountCents := request.AmountCents
	if request.PercentBps != 0 {
		var baseCents int
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(amount_cents), 0) FROM payroll_lines
			  WHERE run_id = ? AND user_id = ? AND kind <> ?`,
			request.RunID, request.UserID, model.PayrollLineAdjustment).Scan(&baseCents); err != nil {
			return err
		}
		if baseCents <= 0 {
			return model.ErrNoPayableTime
		}
		amountCents = percentOfCents(baseCents, request.PercentBps)
		if amountCents == 0 {
			return model.ErrNoPayableTime
		}
		pct := float64(request.PercentBps) / 100.0
		if request.PercentBps%100 == 0 {
			label = fmt.Sprintf("%s (%+d%%)", label, request.PercentBps/100)
		} else {
			label = fmt.Sprintf("%s (%+.2f%%)", label, pct)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents, kind, percent_bps) VALUES (?, ?, ?, 0, 0, ?, ?, ?)`,
		request.RunID, request.UserID, label, amountCents, model.PayrollLineAdjustment, request.PercentBps); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePayrollAdjustment removes a one-off adjustment line from a draft run.
func (d *DB) DeletePayrollAdjustment(ctx context.Context, request appmodel.PayrollAdjustmentDeleteRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return err
	}
	var status string
	if err := tx.QueryRowContext(ctx,
		`SELECT status FROM payroll_runs WHERE id = ? AND team_id = ? FOR UPDATE`, request.RunID, request.TeamID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status != "draft" {
		return model.ErrPayrollNotDraft
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM payroll_lines WHERE id = ? AND run_id = ? AND kind = ?`,
		request.LineID, request.RunID, model.PayrollLineAdjustment)
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
	return tx.Commit()
}

// percentOfCents takes bps basis points of base, rounding half away from zero.
func percentOfCents(base, bps int) int {
	if bps < 0 {
		return (base*bps - 5000) / 10000
	}
	return (base*bps + 5000) / 10000
}

// RecalculatePayrollDraft prices a draft run again from the timesheet as it is
// now, for the people on it. Hours, rates and salaries are taken afresh; a
// fixed bonus or deduction stays as it was, and one given as a percent is taken
// of the new earnings. The same gates as creating a run apply: only approved
// weeks and no closed month. The result carries both totals and the people left
// with no earnings, whose percent adjustments came to zero, so the screen and
// the audit can say what changed.
func (d *DB) RecalculatePayrollDraft(ctx context.Context, request appmodel.PayrollRecalculateRequest) (appmodel.PayrollRecalculateResult, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.PayrollRecalculateResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := recalculatePayrollTx(ctx, tx, request)
	if err != nil {
		return result, err
	}
	// Applied only from a preview: if anyone's amount moved since, the
	// manager confirmed numbers that are no longer the ones being written.
	if request.Expect == "" || request.Expect != result.Fingerprint {
		return result, model.ErrPayrollChanged
	}
	return result, tx.Commit()
}

// PreviewPayrollRecalculation works out what RecalculatePayrollDraft would do
// and writes nothing: the same transaction, rolled back. What the manager
// confirms is exactly what the recalculation computes.
func (d *DB) PreviewPayrollRecalculation(ctx context.Context, request appmodel.PayrollRecalculateRequest) (appmodel.PayrollRecalculateResult, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.PayrollRecalculateResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	return recalculatePayrollTx(ctx, tx, request)
}

// payrollTotalsByUserTx is each person's amount and priced seconds in a run.
func payrollTotalsByUserTx(ctx context.Context, tx *Tx, runID int64) (map[int64]int, map[int64]int, error) {
	cents, secs := map[int64]int{}, map[int64]int{}
	err := queryRows(ctx, tx, "payroll totals by person",
		`SELECT user_id, COALESCE(SUM(amount_cents), 0), COALESCE(SUM(seconds), 0) FROM payroll_lines WHERE run_id = ? GROUP BY user_id`,
		func(rows *sql.Rows) error {
			var id int64
			var c, sec int
			if err := rows.Scan(&id, &c, &sec); err != nil {
				return err
			}
			cents[id], secs[id] = c, sec
			return nil
		}, runID)
	return cents, secs, err
}

func userNameTx(ctx context.Context, tx *Tx, id int64) (string, error) {
	var name string
	err := tx.QueryRowContext(ctx, `SELECT name FROM users WHERE id = ?`, id).Scan(&name)
	return name, err
}

func recalculatePayrollTx(ctx context.Context, tx *Tx, request appmodel.PayrollRecalculateRequest) (appmodel.PayrollRecalculateResult, error) {
	var result appmodel.PayrollRecalculateResult
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return result, err
	}
	if _, err := lockPayrollWorkspace(ctx, tx, request.TeamID); err != nil {
		return result, err
	}
	run, err := getPayrollRun(ctx, tx, request.TeamID, request.RunID, true)
	if err != nil {
		return result, err
	}
	if run.Status != "draft" {
		return result, model.ErrPayrollNotDraft
	}
	if err := requireApprovedPayrollTime(ctx, tx, request.TeamID, run.PeriodStart, run.PeriodEnd); err != nil {
		return result, err
	}
	if err := lockClosedPeriodSpan(ctx, tx, request.TeamID, run.PeriodStart, run.PeriodEnd); err != nil {
		return result, err
	}
	before, beforeSecs, err := payrollTotalsByUserTx(ctx, tx, run.ID)
	if err != nil {
		return result, err
	}
	// A run made before the member list existed has only its lines to say
	// who it is for; they join the list here, once.
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO payroll_run_members (run_id, user_id) SELECT DISTINCT run_id, user_id FROM payroll_lines WHERE run_id = ? ON CONFLICT DO NOTHING`, run.ID); err != nil {
		return result, err
	}
	members := map[int64]bool{}
	if err := queryRows(ctx, tx, "payroll run members", `SELECT user_id FROM payroll_run_members WHERE run_id = ?`, func(rows *sql.Rows) error {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		members[id] = true
		return nil
	}, run.ID); err != nil {
		return result, err
	}
	if err := lockPayrollMembers(ctx, tx, request.TeamID); err != nil {
		return result, err
	}
	lines, err := buildPayrollLines(ctx, tx, request.TeamID, run.PeriodStart, run.PeriodEnd, true, request.At)
	if err != nil {
		return result, fmt.Errorf("build payroll lines: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM payroll_lines WHERE run_id = ? AND kind <> ?`, run.ID, model.PayrollLineAdjustment); err != nil {
		return result, err
	}
	base := map[int64]int{}
	for _, l := range lines {
		if !members[l.UserID] {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO payroll_lines (run_id, user_id, label, seconds, rate_cents, amount_cents) VALUES (?, ?, ?, ?, ?, ?)`,
			run.ID, l.UserID, l.Label, l.Seconds, l.RateCents, l.AmountCents); err != nil {
			return result, err
		}
		base[l.UserID] += l.AmountCents
	}
	// The percent is applied in Go, like when it was added, so both round the
	// same way.
	type percentLine struct {
		id, user int64
		bps      int
	}
	var percents []percentLine
	if err := queryRows(ctx, tx, "percent adjustments",
		`SELECT id, user_id, percent_bps FROM payroll_lines WHERE run_id = ? AND kind = ? AND percent_bps <> 0 ORDER BY id`,
		func(rows *sql.Rows) error {
			var p percentLine
			if err := rows.Scan(&p.id, &p.user, &p.bps); err != nil {
				return err
			}
			percents = append(percents, p)
			return nil
		}, run.ID, model.PayrollLineAdjustment); err != nil {
		return result, err
	}
	// Everyone on the run left with no hours and no salary is named, with or
	// without a percent bonus: a fixed bonus alone is still worth a look.
	zeroed := map[int64]bool{}
	for id := range members {
		if base[id] <= 0 {
			zeroed[id] = true
		}
	}
	for _, p := range percents {
		if _, err := tx.ExecContext(ctx, `UPDATE payroll_lines SET amount_cents = ? WHERE id = ?`, percentOfCents(base[p.user], p.bps), p.id); err != nil {
			return result, err
		}
	}
	for user := range zeroed {
		name, err := userNameTx(ctx, tx, user)
		if err != nil {
			return result, err
		}
		result.NoEarnings = append(result.NoEarnings, name)
	}
	sort.Strings(result.NoEarnings)
	after, afterSecs, err := payrollTotalsByUserTx(ctx, tx, run.ID)
	if err != nil {
		return result, err
	}
	people := map[int64]bool{}
	for id := range members {
		people[id] = true
	}
	for id := range before {
		people[id] = true
	}
	for id := range people {
		name, err := userNameTx(ctx, tx, id)
		if err != nil {
			return result, err
		}
		result.People = append(result.People, appmodel.PayrollRecalculatePerson{
			UserID: id, Name: name, BeforeCents: before[id], AfterCents: after[id],
			BeforeSeconds: beforeSecs[id], AfterSeconds: afterSecs[id],
		})
		result.BeforeCents += before[id]
		result.AfterCents += after[id]
		// Hours count as a change even when the money rounds the same: the
		// run and its CSV would still show the old hours.
		if before[id] != after[id] || beforeSecs[id] != afterSecs[id] {
			result.Changed = true
		}
	}
	sort.Slice(result.People, func(a, b int) bool {
		if result.People[a].Name != result.People[b].Name {
			return result.People[a].Name < result.People[b].Name
		}
		return result.People[a].UserID < result.People[b].UserID
	})
	fp := sha256.New()
	byID := append([]appmodel.PayrollRecalculatePerson(nil), result.People...)
	sort.Slice(byID, func(a, b int) bool { return byID[a].UserID < byID[b].UserID })
	// Before and after both: a confirmation stands for the screen the
	// manager saw, so the journal's "before" is the one they read.
	for _, p := range byID {
		fmt.Fprintf(fp, "%d:%d:%d:%d:%d;", p.UserID, p.BeforeCents, p.BeforeSeconds, p.AfterCents, p.AfterSeconds)
	}
	result.Fingerprint = hex.EncodeToString(fp.Sum(nil)[:12])
	// Who the period would pay but the run is not for: someone who joined
	// after the draft, or was left out of a partial run on purpose. Named,
	// not added: whether they belong on this run is the manager's call.
	outside := map[int64]bool{}
	for _, l := range lines {
		if !members[l.UserID] && l.AmountCents > 0 {
			outside[l.UserID] = true
		}
	}
	for id := range outside {
		name, err := userNameTx(ctx, tx, id)
		if err != nil {
			return result, err
		}
		result.NotInRun = append(result.NotInRun, name)
	}
	sort.Strings(result.NotInRun)
	return result, nil
}
