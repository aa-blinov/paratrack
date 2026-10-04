package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/integrationport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func TestIntegrationWritesAndSecretReads_RecheckManagerRole(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "T", "t")
	ownerID := teamOwner(t, d, teamID)
	createdAt := time.Date(2026, time.October, 3, 12, 30, 0, 0, time.UTC)
	clockReads := 0
	d.now = func() time.Time {
		clockReads++
		return createdAt
	}
	integration, err := d.CreateIntegration(ctx, appmodel.IntegrationCreateRequest{TeamID: teamID, CallerID: ownerID, Provider: "github", Name: "repo", Secret: "secret", Config: appmodel.IntegrationConfig{Target: "owner/repo"}})
	if err != nil {
		t.Fatal(err)
	}
	if clockReads != 1 || !integration.CreatedAt.Equal(createdAt) {
		t.Fatalf("created integration time = %s with %d clock reads, want %s with one read", integration.CreatedAt, clockReads, createdAt)
	}
	var storedCreatedAt string
	if err := d.sql.QueryRowContext(ctx, `SELECT created_at FROM integrations WHERE id = ?`, integration.ID).Scan(&storedCreatedAt); err != nil {
		t.Fatal(err)
	}
	if storedCreatedAt != FormatTime(createdAt) {
		t.Fatalf("stored integration created_at = %q, want %q", storedCreatedAt, FormatTime(createdAt))
	}
	memberID := seedProjectTestUser(t, d, "member")
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}

	if _, err := d.CreateIntegration(ctx, appmodel.IntegrationCreateRequest{TeamID: teamID, CallerID: memberID, Provider: "github", Name: "other", Secret: "secret", Config: appmodel.IntegrationConfig{}}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("CreateIntegration error = %v, want forbidden", err)
	}
	if _, err := d.BeginIntegrationSync(ctx, appmodel.IntegrationSyncStartRequest{TeamID: teamID, IntegrationID: integration.ID, CallerID: memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("BeginIntegrationSync error = %v, want forbidden", err)
	}
	if err := d.DeleteIntegration(ctx, appmodel.IntegrationMutationRequest{TeamID: teamID, IntegrationID: integration.ID, CallerID: memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("DeleteIntegration error = %v, want forbidden", err)
	}
	if err := d.SyncExternalTasks(ctx, appmodel.IntegrationTaskSyncRequest{TeamID: teamID, IntegrationID: integration.ID, CallerID: memberID, Tasks: []integrationport.ProviderTask{{
		ExternalID: "task-1", Title: "task", Status: "open",
	}}}); !errors.Is(err, model.ErrForbidden) {
		t.Errorf("SyncExternalTasks error = %v, want forbidden", err)
	}

	credentials, err := d.BeginIntegrationSync(ctx, appmodel.IntegrationSyncStartRequest{TeamID: teamID, IntegrationID: integration.ID, CallerID: ownerID})
	if err != nil {
		t.Fatalf("integration was changed by denied operation: %v", err)
	}
	if credentials.Config.Target != "owner/repo" {
		t.Fatalf("integration sync target = %q, want %q", credentials.Config.Target, "owner/repo")
	}
}

func TestBeginIntegrationSyncRejectsMalformedConfigWithoutAdvancingGeneration(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Malformed config", "malformed-config")
	ownerID := teamOwner(t, d, teamID)
	integration, err := d.CreateIntegration(ctx, appmodel.IntegrationCreateRequest{
		TeamID: teamID, CallerID: ownerID, Provider: "github", Name: "repo", Secret: "secret",
		Config: appmodel.IntegrationConfig{Target: "owner/repo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE integrations SET config = ? WHERE id = ?`, "{broken", integration.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.BeginIntegrationSync(ctx, appmodel.IntegrationSyncStartRequest{
		TeamID: teamID, IntegrationID: integration.ID, CallerID: ownerID,
	}); err == nil {
		t.Fatal("BeginIntegrationSync accepted malformed stored configuration")
	}
	var generation int64
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT sync_generation FROM integrations WHERE id = ?`, integration.ID).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation != 0 {
		t.Fatalf("sync generation = %d, want 0 after configuration decode failure", generation)
	}
}

func TestCreateScopedAPIToken_RechecksMembershipInPersistence(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Scoped token", "scoped-token")
	memberID := seedProjectTestUser(t, d, "scoped-token-member")
	ctx = requestctx.WithActor(ctx, memberID)
	if _, err := d.TestSQL().ExecContext(ctx,
		`INSERT INTO memberships (team_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`,
		teamID, memberID, FormatTime(time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.CreateAPIToken(ctx, appmodel.APITokenCreateRequest{UserID: memberID, CallerID: memberID, Name: "before removal", Options: TokenOptions{TeamID: teamID}}); err != nil {
		t.Fatalf("create token for current member: %v", err)
	}
	if _, err := d.TestSQL().ExecContext(ctx, `DELETE FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, memberID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.CreateAPIToken(ctx, appmodel.APITokenCreateRequest{UserID: memberID, CallerID: memberID, Name: "after removal", Options: TokenOptions{TeamID: teamID}}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("create token after membership removal error = %v, want forbidden", err)
	}
	var count int
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT count(*) FROM api_tokens WHERE user_id = ? AND team_id = ?`, memberID, teamID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("scoped token count = %d, want only the token created before removal", count)
	}
}

func TestSyncExternalTasksUpsertsSnapshotAndClosesMissingTasks(t *testing.T) {
	d := openTestDB(t)
	ctx := t.Context()
	teamID := seedTeam(t, d, "Sync snapshot", "sync-snapshot")
	ownerID := teamOwner(t, d, teamID)
	integration, err := d.CreateIntegration(ctx, appmodel.IntegrationCreateRequest{TeamID: teamID, CallerID: ownerID, Provider: "github", Name: "repo", Secret: "secret", Config: appmodel.IntegrationConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	first := []integrationport.ProviderTask{
		{ExternalID: "task-1", Title: "old title", Status: "open"},
		{ExternalID: "task-2", Title: "keep title", Status: "open"},
		{ExternalID: "task-1", Title: "latest title", Status: "in progress"},
	}
	firstSync, err := d.BeginIntegrationSync(ctx, appmodel.IntegrationSyncStartRequest{TeamID: teamID, IntegrationID: integration.ID, CallerID: ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SyncExternalTasks(ctx, appmodel.IntegrationTaskSyncRequest{TeamID: teamID, IntegrationID: integration.ID, CallerID: ownerID, Generation: firstSync.Generation, Tasks: first}); err != nil {
		t.Fatalf("sync initial snapshot: %v", err)
	}
	tasks, err := d.ListExternalTasks(ctx, teamID, integration.ID)
	if err != nil {
		t.Fatalf("list initial snapshot: %v", err)
	}
	byExternalID := make(map[string]ExternalTask, len(tasks))
	for _, task := range tasks {
		byExternalID[task.ExternalID] = task
	}
	if len(byExternalID) != 2 || byExternalID["task-1"].Title != "latest title" || byExternalID["task-1"].Status != "in progress" {
		t.Fatalf("initial snapshot = %+v, want 2 tasks with last duplicate winning", byExternalID)
	}

	secondSync, err := d.BeginIntegrationSync(ctx, appmodel.IntegrationSyncStartRequest{TeamID: teamID, IntegrationID: integration.ID, CallerID: ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SyncExternalTasks(ctx, appmodel.IntegrationTaskSyncRequest{TeamID: teamID, IntegrationID: integration.ID, CallerID: ownerID, Generation: secondSync.Generation, Tasks: first[1:2]}); err != nil {
		t.Fatalf("sync reduced snapshot: %v", err)
	}
	if err := d.SyncExternalTasks(ctx, appmodel.IntegrationTaskSyncRequest{TeamID: teamID, IntegrationID: integration.ID, CallerID: ownerID, Generation: firstSync.Generation, Tasks: first}); !errors.Is(err, appmodel.ErrIntegrationSyncSuperseded) {
		t.Fatalf("stale snapshot error = %v, want superseded", err)
	}
	tasks, err = d.ListExternalTasks(ctx, teamID, integration.ID)
	if err != nil {
		t.Fatalf("list reduced snapshot: %v", err)
	}
	byExternalID = make(map[string]ExternalTask, len(tasks))
	for _, task := range tasks {
		byExternalID[task.ExternalID] = task
	}
	if byExternalID["task-1"].Status != "closed" || byExternalID["task-2"].Status != "open" {
		t.Fatalf("reconciled snapshot = %+v, want task-1 closed and task-2 open", byExternalID)
	}
}
