package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// maxTimeEditReason bounds the reason a person gives for a change (TIME-19).
const maxTimeEditReason = 500

// CreateTimeEditRequest records a member's request to change one of their timesheet cells.
// The activity must belong to the workspace; nothing is written to the cell until a manager
// approves it (TIME-19).
func (d *DB) CreateTimeEditRequest(ctx context.Context, request appmodel.TimeEditCreateRequest) (appmodel.TimeEditRequest, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.ActivityID <= 0 {
		return appmodel.TimeEditRequest{}, ErrNotFound
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" || len([]rune(reason)) > maxTimeEditReason || request.Minutes < 0 || request.Minutes > 24*60 || request.Day.IsZero() {
		return appmodel.TimeEditRequest{}, appmodel.ErrInvalidTask
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.TimeEditRequest{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return appmodel.TimeEditRequest{}, err
	}
	var activityName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM activities WHERE id = ? AND team_id = ?`, request.ActivityID, request.TeamID).Scan(&activityName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appmodel.TimeEditRequest{}, ErrNotFound
		}
		return appmodel.TimeEditRequest{}, err
	}
	now := d.currentTime().UTC()
	day := request.Day.Format("2006-01-02")
	var id int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO time_edit_requests (team_id, user_id, activity_id, day, minutes, reason, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'pending', ?) RETURNING id`,
		request.TeamID, request.CallerID, request.ActivityID, day, request.Minutes, reason, FormatTime(now)).Scan(&id); err != nil {
		return appmodel.TimeEditRequest{}, fmt.Errorf("insert time edit request: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return appmodel.TimeEditRequest{}, err
	}
	return appmodel.TimeEditRequest{
		ID: id, UserID: request.CallerID, ActivityID: request.ActivityID, ActivityName: activityName,
		Day: request.Day, Minutes: request.Minutes, Reason: reason, Status: "pending", CreatedAt: now,
	}, nil
}

// ListTimeEditRequests lists the requests of a workspace, newest first. A manager sees everyone's;
// anyone else sees only their own (TIME-19).
func (d *DB) ListTimeEditRequests(ctx context.Context, teamID, viewerID int64, all bool) ([]appmodel.TimeEditRequest, error) {
	if teamID <= 0 || viewerID <= 0 {
		return nil, ErrNotFound
	}
	allFlag := 0
	if all {
		allFlag = 1
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT r.id, r.user_id, COALESCE(NULLIF(u.name, ''), u.email), r.activity_id, a.name, r.day, r.minutes,
		        r.reason, r.status, r.decision_note, r.created_at
		   FROM time_edit_requests r
		   JOIN users u ON u.id = r.user_id
		   JOIN activities a ON a.id = r.activity_id
		  WHERE r.team_id = ? AND (? = 1 OR r.user_id = ?)
		  ORDER BY r.id DESC LIMIT 200`,
		teamID, allFlag, viewerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []appmodel.TimeEditRequest{}
	for rows.Next() {
		var r appmodel.TimeEditRequest
		var day, created string
		if err := rows.Scan(&r.ID, &r.UserID, &r.Person, &r.ActivityID, &r.ActivityName, &day, &r.Minutes,
			&r.Reason, &r.Status, &r.DecisionNote, &created); err != nil {
			return nil, err
		}
		if r.Day, err = time.Parse("2006-01-02", day); err != nil {
			return nil, err
		}
		if r.CreatedAt, err = ScanTime(created); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DecideTimeEditRequest approves or refuses a pending request. Only a manager decides. An approval
// writes the cell as the person who asked, through the ordinary timesheet write, so the approved
// week lock and the cell rules apply as they do for the person (TIME-19).
func (d *DB) DecideTimeEditRequest(ctx context.Context, decision appmodel.TimeEditDecisionRequest) error {
	if decision.TeamID <= 0 || decision.CallerID <= 0 || decision.RequestID <= 0 {
		return ErrNotFound
	}
	note := strings.TrimSpace(decision.Note)
	if len([]rune(note)) > maxTimeEditReason {
		return appmodel.ErrInvalidTask
	}
	if err := d.checkManagerOf(ctx, decision.TeamID, decision.CallerID); err != nil {
		return err
	}
	var requesterID, activityID int64
	var day string
	var minutes int
	var status string
	if err := d.sql.QueryRowContext(ctx,
		`SELECT user_id, activity_id, day, minutes, status FROM time_edit_requests WHERE id = ? AND team_id = ?`,
		decision.RequestID, decision.TeamID).Scan(&requesterID, &activityID, &day, &minutes, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status != "pending" {
		return appmodel.ErrInvalidTask
	}
	if decision.Approve {
		when, err := time.Parse("2006-01-02", day)
		if err != nil {
			return err
		}
		// The cell is written as the person who asked: UpsertDayTotal owns its transaction, and a
		// set of minutes is idempotent, so a repeated approval writes the same cell again.
		if err := d.UpsertDayTotal(requestctx.WithActor(ctx, requesterID), appmodel.TimesheetCellUpdateRequest{
			TeamID: decision.TeamID, ActivityID: activityID, Day: when, TotalSeconds: minutes * 60,
		}); err != nil {
			return err
		}
	}
	next := "rejected"
	if decision.Approve {
		next = "approved"
	}
	res, err := d.sql.ExecContext(ctx,
		`UPDATE time_edit_requests SET status = ?, decided_by = ?, decision_note = ?, decided_at = ?
		  WHERE id = ? AND team_id = ? AND status = 'pending'`,
		next, decision.CallerID, note, FormatTime(d.currentTime().UTC()), decision.RequestID, decision.TeamID)
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
	return nil
}

// checkManagerOf reads the caller's role in the workspace and refuses anyone who is not a manager.
func (d *DB) checkManagerOf(ctx context.Context, teamID, callerID int64) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	return tx.Commit()
}
