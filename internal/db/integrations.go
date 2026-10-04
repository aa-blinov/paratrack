package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------
// Integrations (github / trello)
// ---------------------------------------------------------------------------

// Integration records are returned as credential-free summaries.
type ExternalTask = model.ExternalTask
type ExternalTaskWithProvider = model.ExternalTaskWithProvider

type integrationConfigRecord struct {
	Target string `json:"target,omitempty"`
}

// CreateIntegration stores a connection. name is the display label
// (e.g. "acme/api-server" or "Product board").
func (d *DB) CreateIntegration(ctx context.Context, request appmodel.IntegrationCreateRequest) (model.IntegrationSummary, error) {
	teamID, callerID := request.TeamID, request.CallerID
	provider, name, secret := request.Provider, request.Name, request.Secret
	if teamID <= 0 || callerID <= 0 {
		return model.IntegrationSummary{}, ErrNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return model.IntegrationSummary{}, fmt.Errorf("name is required")
	}
	sealedSecret, err := d.sealSecret(secret)
	if err != nil {
		return model.IntegrationSummary{}, fmt.Errorf("seal integration secret: %w", err)
	}
	configJSON, err := json.Marshal(integrationConfigRecord{Target: request.Config.Target})
	if err != nil {
		return model.IntegrationSummary{}, fmt.Errorf("encode integration configuration: %w", err)
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.IntegrationSummary{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return model.IntegrationSummary{}, err
	}
	createdAt := d.currentTime().UTC()
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO integrations (team_id, provider, name, secret, config, created_at)
		 VALUES (?, ?, ?, ?, ?, ?) RETURNING id`,
		teamID, provider, name, sealedSecret, string(configJSON), FormatTime(createdAt)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return model.IntegrationSummary{}, ErrDuplicate
		}
		return model.IntegrationSummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.IntegrationSummary{}, err
	}
	return model.IntegrationSummary{ID: id, TeamID: teamID, Provider: provider, Name: name, CreatedAt: createdAt}, nil
}

// ListIntegrations returns safe summaries; credentials are only read by the
// team-scoped sync starter used when a provider fetch begins.
func (d *DB) ListIntegrations(ctx context.Context, teamID int64) ([]model.IntegrationSummary, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT id, team_id, provider, name, created_at
		 FROM integrations WHERE team_id = ? ORDER BY provider, name`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.IntegrationSummary
	for rows.Next() {
		var (
			it      model.IntegrationSummary
			created string
		)
		if err := rows.Scan(&it.ID, &it.TeamID, &it.Provider, &it.Name, &created); err != nil {
			return nil, err
		}
		it.CreatedAt, err = ScanTime(created)
		if err != nil {
			return nil, fmt.Errorf("parse integration creation time: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// BeginIntegrationSync reserves the newest snapshot generation and returns
// its credentials atomically. A later call invalidates every earlier fetch.
func (d *DB) BeginIntegrationSync(ctx context.Context, request appmodel.IntegrationSyncStartRequest) (appmodel.IntegrationSyncCredentials, error) {
	if request.TeamID <= 0 || request.IntegrationID <= 0 || request.CallerID <= 0 {
		return appmodel.IntegrationSyncCredentials{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return appmodel.IntegrationSyncCredentials{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return appmodel.IntegrationSyncCredentials{}, err
	}
	var it appmodel.IntegrationSyncCredentials
	var configJSON string
	err = tx.QueryRowContext(ctx, `UPDATE integrations
		SET sync_generation = sync_generation + 1
		WHERE id = ? AND team_id = ?
		RETURNING id, team_id, provider, secret, config, sync_generation`,
		request.IntegrationID, request.TeamID).Scan(&it.ID, &it.TeamID, &it.Provider, &it.Secret, &configJSON, &it.Generation)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appmodel.IntegrationSyncCredentials{}, ErrNotFound
		}
		return appmodel.IntegrationSyncCredentials{}, fmt.Errorf("reserve integration sync: %w", err)
	}
	if strings.TrimSpace(configJSON) == "" {
		configJSON = "{}"
	}
	var config integrationConfigRecord
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return appmodel.IntegrationSyncCredentials{}, fmt.Errorf("decode integration configuration: %w", err)
	}
	it.Config = appmodel.IntegrationConfig{Target: config.Target}
	it.Secret, err = d.openSecret(it.Secret)
	if err != nil {
		return appmodel.IntegrationSyncCredentials{}, fmt.Errorf("open integration credential: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return appmodel.IntegrationSyncCredentials{}, err
	}
	return it, nil
}

// GetIntegrationSummary reads one connection without decrypting its secret.
func (d *DB) GetIntegrationSummary(ctx context.Context, query appmodel.IntegrationLookupQuery) (model.IntegrationSummary, error) {
	if query.TeamID <= 0 || query.IntegrationID <= 0 {
		return model.IntegrationSummary{}, ErrNotFound
	}
	var (
		item    model.IntegrationSummary
		created string
	)
	err := d.sql.QueryRowContext(ctx,
		`SELECT id, team_id, provider, name, created_at
		 FROM integrations WHERE id = ? AND team_id = ?`, query.IntegrationID, query.TeamID,
	).Scan(&item.ID, &item.TeamID, &item.Provider, &item.Name, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.IntegrationSummary{}, ErrNotFound
		}
		return model.IntegrationSummary{}, err
	}
	item.CreatedAt, err = ScanTime(created)
	if err != nil {
		return model.IntegrationSummary{}, fmt.Errorf("parse integration creation time: %w", err)
	}
	return item, nil
}

// DeleteIntegration removes the connection and its imported tasks.
func (d *DB) DeleteIntegration(ctx context.Context, request appmodel.IntegrationMutationRequest) error {
	if request.TeamID <= 0 {
		return ErrNotFound
	}
	teamID, id, callerID := request.TeamID, request.IntegrationID, request.CallerID
	if id <= 0 || callerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM integrations WHERE id = ? AND team_id = ?`, id, teamID)
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

// ListExternalTasks returns imported items for a workspace integration.
func (d *DB) ListExternalTasks(ctx context.Context, query appmodel.IntegrationLookupQuery) ([]ExternalTask, error) {
	if query.TeamID <= 0 || query.IntegrationID <= 0 {
		return nil, ErrNotFound
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT t.id, t.integration_id, t.external_id, t.title, t.url, t.status, COALESCE(t.activity_id, 0)
		 FROM external_tasks t JOIN integrations i ON i.id = t.integration_id
		 WHERE i.team_id = ? AND t.integration_id = ? ORDER BY t.status, t.title`, query.TeamID, query.IntegrationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExternalTask
	for rows.Next() {
		t, err := scanExternalTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetExternalTask fetches one imported task only when its integration belongs
// to the requested workspace.
func (d *DB) GetExternalTask(ctx context.Context, query appmodel.ExternalTaskLookupQuery) (ExternalTask, error) {
	if query.TeamID <= 0 || query.TaskID <= 0 {
		return ExternalTask{}, ErrNotFound
	}
	task, err := scanExternalTask(d.sql.QueryRowContext(ctx,
		`SELECT t.id, t.integration_id, t.external_id, t.title, t.url, t.status, COALESCE(t.activity_id, 0)
		 FROM external_tasks t JOIN integrations i ON i.id = t.integration_id
		 WHERE t.id = ? AND i.team_id = ?`, query.TaskID, query.TeamID))
	if errors.Is(err, sql.ErrNoRows) {
		return ExternalTask{}, ErrNotFound
	}
	return task, err
}

// ListExternalTasksForTeam loads every imported task and provider for a team
// in one query, avoiding per-integration lookup loops in HTTP adapters.
func (d *DB) ListExternalTasksForTeam(ctx context.Context, teamID int64) ([]ExternalTaskWithProvider, error) {
	rows, err := d.sql.QueryContext(ctx, `
		SELECT t.id, t.integration_id, t.title, t.url, t.status, i.provider
		FROM external_tasks t JOIN integrations i ON i.id = t.integration_id
		WHERE i.team_id = ? ORDER BY t.status, t.title`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExternalTaskWithProvider
	for rows.Next() {
		var task ExternalTaskWithProvider
		if err := rows.Scan(&task.ID, &task.IntegrationID, &task.Title, &task.URL, &task.Status, &task.Provider); err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func scanExternalTask(r interface{ Scan(...any) error }) (ExternalTask, error) {
	var t ExternalTask
	if err := r.Scan(&t.ID, &t.IntegrationID, &t.ExternalID, &t.Title, &t.URL, &t.Status, &t.ActivityID); err != nil {
		return ExternalTask{}, err
	}
	return t, nil
}
