package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The confirmation queue (agent-action-queue). The rules below decide who may ask and
// who may decide, and they are checked in the same transaction as the write, so a route
// guard cannot be the only check. A request is decided once; only the decision and the
// outcome are written to it afterwards.

// agentActionAllowed is the role an action needs: the owner for a workspace deletion,
// a manager for the other three.
func agentActionAllowed(kind string, role model.TeamRole) error {
	switch kind {
	case appmodel.AgentActionWorkspaceDelete:
		if !role.IsOwner() {
			return model.ErrForbidden
		}
	case appmodel.AgentActionInvoiceDelete, appmodel.AgentActionPayrollRunDelete, appmodel.AgentActionWeekReopen:
		if !role.CanManage() {
			return model.ErrForbidden
		}
	default:
		return appmodel.FieldError{Field: "kind"}
	}
	return nil
}

// CreateAgentAction queues a request. The target must exist in the workspace and be in a
// state the action can still apply to; a request that is still waiting for the same target
// returns that request, so a retry does not queue twice.
func (d *DB) CreateAgentAction(ctx context.Context, request appmodel.AgentActionCreateRequest) (appmodel.AgentActionView, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return appmodel.AgentActionView{}, ErrNotFound
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > appmodel.AgentActionReasonMax {
		return appmodel.AgentActionView{}, appmodel.FieldError{Field: "reason"}
	}
	// A week is found among the approvals before the transaction: the lookup reads
	// across the week's own days, and the request keeps the instant it found.
	var weekID int64
	var weekLabel string
	if request.Kind == appmodel.AgentActionWeekReopen {
		var err error
		if weekID, weekLabel, err = d.approvedWeekTarget(ctx, request); err != nil {
			return appmodel.AgentActionView{}, err
		}
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.AgentActionView{}, err
	}
	defer func() { _ = tx.Rollback() }()
	_, role, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return appmodel.AgentActionView{}, err
	}
	if err := agentActionAllowed(request.Kind, role); err != nil {
		return appmodel.AgentActionView{}, err
	}
	targetID, label := weekID, weekLabel
	if request.Kind != appmodel.AgentActionWeekReopen {
		if targetID, label, err = agentActionTarget(ctx, tx, request); err != nil {
			return appmodel.AgentActionView{}, err
		}
	}
	if existing, found, err := findPendingAgentAction(ctx, tx, request.TeamID, request.Kind, targetID); err != nil || found {
		return existing, err
	}
	now := d.currentTime().UTC()
	view := appmodel.AgentActionView{
		TeamID: request.TeamID, Kind: request.Kind, TargetID: targetID, TargetLabel: label, Reason: reason,
		Status: "pending", RequestedBy: request.CallerID, CreatedAt: now,
	}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO agent_actions (team_id, requested_by, kind, target_id, target_label, reason, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'pending', ?) RETURNING id`,
		request.TeamID, request.CallerID, request.Kind, targetID, label, reason, FormatTime(now)).Scan(&view.ID); err != nil {
		return appmodel.AgentActionView{}, err
	}
	if err := tx.Commit(); err != nil {
		return appmodel.AgentActionView{}, err
	}
	return view, nil
}

// agentActionTarget reads the invoice, pay run or workspace a request names, and the
// label the person sees. The state check is the one the page's action applies.
func agentActionTarget(ctx context.Context, q queryRower, request appmodel.AgentActionCreateRequest) (int64, string, error) {
	switch request.Kind {
	case appmodel.AgentActionInvoiceDelete:
		var number, status string
		err := q.QueryRowContext(ctx, `SELECT number, status FROM invoices WHERE id = ? AND team_id = ?`,
			request.TargetID, request.TeamID).Scan(&number, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", ErrNotFound
		}
		if err != nil {
			return 0, "", err
		}
		if status != "draft" {
			return 0, "", model.ErrInvoiceNotDraft
		}
		return request.TargetID, number, nil
	case appmodel.AgentActionPayrollRunDelete:
		var number, status string
		err := q.QueryRowContext(ctx, `SELECT number, status FROM payroll_runs WHERE id = ? AND team_id = ?`,
			request.TargetID, request.TeamID).Scan(&number, &status)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", ErrNotFound
		}
		if err != nil {
			return 0, "", err
		}
		if status != "draft" {
			return 0, "", appmodel.ErrPayrollRunNotDraft
		}
		return request.TargetID, number, nil
	case appmodel.AgentActionWorkspaceDelete:
		var name string
		err := q.QueryRowContext(ctx, `SELECT name FROM teams WHERE id = ?`, request.TeamID).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", ErrNotFound
		}
		if err != nil {
			return 0, "", err
		}
		return request.TeamID, name, nil
	}
	return 0, "", appmodel.FieldError{Field: "kind"}
}

// approvedWeekTarget finds the approved week that starts on the request's day, in the
// zone the request carries. It returns the approval's own start instant, which is what
// the reopen later matches, and the day the person reads.
func (d *DB) approvedWeekTarget(ctx context.Context, request appmodel.AgentActionCreateRequest) (int64, string, error) {
	day := request.WeekStart
	approvals, err := d.ListTimesheetApprovals(ctx, request.TeamID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 8))
	if err != nil {
		return 0, "", err
	}
	want := day.Format(time.DateOnly)
	for _, approval := range approvals {
		if approval.Start.In(day.Location()).Format(time.DateOnly) == want {
			return approval.Start.Unix(), want, nil
		}
	}
	return 0, "", appmodel.ErrAgentWeekNotApproved
}

// findPendingAgentAction returns the request still waiting for this target, if there is one.
func findPendingAgentAction(ctx context.Context, q queryRower, teamID int64, kind string, targetID int64) (appmodel.AgentActionView, bool, error) {
	var view appmodel.AgentActionView
	var created string
	err := q.QueryRowContext(ctx,
		`SELECT id, requested_by, target_label, reason, created_at FROM agent_actions
		  WHERE team_id = ? AND kind = ? AND target_id = ? AND status = 'pending' ORDER BY id LIMIT 1`,
		teamID, kind, targetID).Scan(&view.ID, &view.RequestedBy, &view.TargetLabel, &view.Reason, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return appmodel.AgentActionView{}, false, nil
	}
	if err != nil {
		return appmodel.AgentActionView{}, false, err
	}
	view.TeamID, view.Kind, view.TargetID, view.Status = teamID, kind, targetID, "pending"
	if view.CreatedAt, err = ParseTime(created); err != nil {
		return appmodel.AgentActionView{}, false, err
	}
	return view, true, nil
}

// ListPendingAgentActions lists the requests still waiting for a decision, oldest first.
// Only a manager reads the queue.
func (d *DB) ListPendingAgentActions(ctx context.Context, query appmodel.AgentActionListQuery) ([]appmodel.AgentActionView, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return nil, ErrNotFound
	}
	role, err := readTeamRole(ctx, d.sql, query.TeamID, query.CallerID)
	if err != nil {
		return nil, err
	}
	if !role.CanManage() {
		return nil, model.ErrForbidden
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT a.id, a.kind, a.target_id, a.target_label, a.reason, a.requested_by,
		        COALESCE(NULLIF(u.name, ''), u.email, ''), a.created_at, COALESCE(a.decided_at, ''), a.decided_by
		   FROM agent_actions a LEFT JOIN users u ON u.id = a.requested_by
		  WHERE a.team_id = ? AND a.status = 'pending' ORDER BY a.id`, query.TeamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]appmodel.AgentActionView, 0)
	for rows.Next() {
		view := appmodel.AgentActionView{TeamID: query.TeamID, Status: "pending"}
		var created, decided string
		if err := rows.Scan(&view.ID, &view.Kind, &view.TargetID, &view.TargetLabel, &view.Reason, &view.RequestedBy,
			&view.RequestedByName, &created, &decided, &view.DecidedBy); err != nil {
			return nil, err
		}
		if view.CreatedAt, err = ParseTime(created); err != nil {
			return nil, err
		}
		if decided != "" {
			if view.DecidedAt, err = ParseTime(decided); err != nil {
				return nil, err
			}
		}
		out = append(out, view)
	}
	return out, rows.Err()
}

// DecideAgentAction confirms a pending request, claiming it for the caller, or rejects it.
// A rejection is final. A confirmation only claims the request: the caller carries out the
// action and then reports the outcome with FinishAgentAction. A claim that is still inside
// its window refuses a second confirmation.
func (d *DB) DecideAgentAction(ctx context.Context, request appmodel.AgentActionDecisionRequest) (appmodel.AgentActionView, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.ActionID <= 0 {
		return appmodel.AgentActionView{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.AgentActionView{}, err
	}
	defer func() { _ = tx.Rollback() }()
	_, role, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID)
	if err != nil {
		return appmodel.AgentActionView{}, err
	}
	view, err := lockAgentAction(ctx, tx, request.TeamID, request.ActionID)
	if err != nil {
		return appmodel.AgentActionView{}, err
	}
	if view.Status != "pending" {
		return appmodel.AgentActionView{}, appmodel.ErrAgentActionDecided
	}
	if err := agentActionAllowed(view.Kind, role); err != nil {
		return appmodel.AgentActionView{}, err
	}
	now := d.currentTime().UTC()
	if !view.DecidedAt.IsZero() && now.Sub(view.DecidedAt) < appmodel.AgentActionClaimWindow {
		return appmodel.AgentActionView{}, appmodel.ErrAgentActionClaimed
	}
	if request.Reject {
		err = execRequireRows(ctx, tx,
			`UPDATE agent_actions SET status = 'rejected', decided_by = ?, decided_at = ? WHERE id = ? AND team_id = ? AND status = 'pending'`,
			request.CallerID, FormatTime(now), request.ActionID, request.TeamID)
		view.Status = "rejected"
	} else {
		err = execRequireRows(ctx, tx,
			`UPDATE agent_actions SET decided_by = ?, decided_at = ? WHERE id = ? AND team_id = ? AND status = 'pending'`,
			request.CallerID, FormatTime(now), request.ActionID, request.TeamID)
	}
	if err != nil {
		return appmodel.AgentActionView{}, err
	}
	if err := tx.Commit(); err != nil {
		return appmodel.AgentActionView{}, err
	}
	view.DecidedBy, view.DecidedAt = request.CallerID, now
	return view, nil
}

// lockAgentAction reads a request under its row lock, so two decisions cannot both see it pending.
func lockAgentAction(ctx context.Context, tx *Tx, teamID, id int64) (appmodel.AgentActionView, error) {
	var view appmodel.AgentActionView
	var created, decided string
	err := tx.QueryRowContext(ctx,
		`SELECT id, kind, target_id, target_label, reason, status, requested_by, created_at, COALESCE(decided_at, ''), decided_by
		   FROM agent_actions WHERE id = ? AND team_id = ? FOR UPDATE`, id, teamID).Scan(
		&view.ID, &view.Kind, &view.TargetID, &view.TargetLabel, &view.Reason, &view.Status,
		&view.RequestedBy, &created, &decided, &view.DecidedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return appmodel.AgentActionView{}, ErrNotFound
	}
	if err != nil {
		return appmodel.AgentActionView{}, err
	}
	view.TeamID = teamID
	if view.CreatedAt, err = ParseTime(created); err != nil {
		return appmodel.AgentActionView{}, err
	}
	if decided != "" {
		if view.DecidedAt, err = ParseTime(decided); err != nil {
			return appmodel.AgentActionView{}, err
		}
	}
	return view, nil
}

// FinishAgentAction writes the outcome of a confirmed request. It only applies to the claim
// the caller holds: the request must still be pending and carry that claim's decision time.
// The row is not touched again after this.
func (d *DB) FinishAgentAction(ctx context.Context, request appmodel.AgentActionFinishRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.ActionID <= 0 {
		return ErrNotFound
	}
	if request.Status != "done" && request.Status != "failed" {
		return appmodel.FieldError{Field: "status"}
	}
	return execRequireRows(ctx, d.sql,
		`UPDATE agent_actions SET status = ?, error = ?
		  WHERE id = ? AND team_id = ? AND status = 'pending' AND decided_by = ? AND decided_at = ?`,
		request.Status, request.Error, request.ActionID, request.TeamID, request.CallerID, FormatTime(request.ClaimedAt))
}
