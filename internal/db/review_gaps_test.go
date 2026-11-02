package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A timer still running in the period is priced at now, so it must sit in an
// approved week like closed time. A pay run that would count it is refused.
func TestPayRunRefusesRunningTimerInUnapprovedTime(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ownerID := seedApprovalUser(t, d, "running-owner@test.local")
	teamID, err := d.CreateOwnedTeam(ctx, appmodel.TeamCreateRequest{Slug: "running-team", Name: "Команда", OwnerID: ownerID})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE memberships SET hourly_pay_cents = 4000 WHERE team_id = ? AND user_id = ?`, teamID, ownerID); err != nil {
		t.Fatalf("set rate: %v", err)
	}
	var activityID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`INSERT INTO activities (name, team_id) VALUES ('Вёрстка', ?) RETURNING id`, teamID).Scan(&activityID); err != nil {
		t.Fatalf("activity: %v", err)
	}
	start := time.Date(2020, 3, 2, 0, 0, 0, 0, time.UTC)
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, note, paused, accumulated_seconds, updated_at)
		 VALUES (?, ?, ?, ?, '', 0, 0, ?)`,
		activityID, teamID, ownerID, FormatTime(start.Add(10*time.Hour)), FormatTime(start)); err != nil {
		t.Fatalf("running timer: %v", err)
	}

	_, _, err = d.CreatePayrollDraft(ctx, appmodel.PayrollDraftRequest{
		TeamID: teamID, CallerID: ownerID, Start: start, End: start.AddDate(0, 0, 7), CreatedAt: time.Now().UTC(),
	})
	var notApproved *model.PayrollNotApprovedError
	if !errors.As(err, &notApproved) {
		t.Fatalf("pay run over a running timer = %v, want PayrollNotApprovedError", err)
	}
}

// A password change ends the account's API tokens, so a token minted under the
// old password stops working.
func TestPasswordChangeRevokesAPITokens(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	userID := seedApprovalUser(t, d, "token-owner@test.local")
	var oldHash string
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&oldHash); err != nil {
		t.Fatalf("read hash: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO api_tokens (user_id, name, token_hash, prefix, created_at) VALUES (?, 'cli', 'hash-test-1', 'pt_test', ?)`,
		userID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	actorCtx := requestctx.WithActor(ctx, userID)
	updated, err := d.UpdateUserPasswordIfHashMatches(actorCtx, appmodel.PasswordHashUpdateRequest{
		UserID: userID, CallerID: userID, ExpectedHash: oldHash, PasswordHash: "new-hash",
	})
	if err != nil || !updated {
		t.Fatalf("change password = %v, %v; want updated", updated, err)
	}
	var left int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM api_tokens WHERE user_id = ?`, userID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("api tokens after password change = %d, want 0", left)
	}
}
