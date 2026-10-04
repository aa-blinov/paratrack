package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// SyncExternalTasks atomically records a provider snapshot only if no newer
// sync has started since its credentials were reserved.
func (d *DB) SyncExternalTasks(ctx context.Context, request appmodel.IntegrationTaskSyncRequest) error {
	teamID, integrationID, callerID, tasks := request.TeamID, request.IntegrationID, request.CallerID, request.Tasks
	if teamID <= 0 || integrationID <= 0 || callerID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin integration sync: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := lockTeamManager(ctx, tx, teamID, callerID); err != nil {
		return err
	}
	var currentGeneration int64
	if err := tx.QueryRowContext(ctx,
		`SELECT sync_generation FROM integrations WHERE id = ? AND team_id = ? FOR UPDATE`, integrationID, teamID).Scan(&currentGeneration); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock integration: %w", err)
	}
	if request.Generation <= 0 || request.Generation != currentGeneration {
		return appmodel.ErrIntegrationSyncSuperseded
	}

	now := FormatTime(d.currentTime().UTC())
	taskIDs := make([]string, 0, len(tasks))
	titles := make([]string, 0, len(tasks))
	urls := make([]string, 0, len(tasks))
	statuses := make([]string, 0, len(tasks))
	positions := make(map[string]int, len(tasks))
	for _, task := range tasks {
		if index, exists := positions[task.ExternalID]; exists {
			// Preserve the previous sequential upsert behavior: the last
			// duplicate provider row wins.
			titles[index], urls[index], statuses[index] = task.Title, task.URL, task.Status
			continue
		}
		positions[task.ExternalID] = len(taskIDs)
		taskIDs = append(taskIDs, task.ExternalID)
		titles = append(titles, task.Title)
		urls = append(urls, task.URL)
		statuses = append(statuses, task.Status)
	}
	if len(taskIDs) > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO external_tasks (integration_id, external_id, title, url, status, created_at)
			SELECT ?, task.external_id, task.title, task.url, task.status, ?
			FROM unnest(?::text[], ?::text[], ?::text[], ?::text[])
			     AS task(external_id, title, url, status)
			ON CONFLICT (integration_id, external_id) DO UPDATE SET
			  title = excluded.title,
			  url = excluded.url,
			  status = excluded.status`,
			integrationID, now, taskIDs, titles, urls, statuses); err != nil {
			return fmt.Errorf("upsert external task snapshot: %w", err)
		}
	}

	query := `UPDATE external_tasks SET status = 'closed' WHERE integration_id = ? AND status <> 'closed'`
	args := []any{integrationID}
	if len(taskIDs) > 0 {
		query += ` AND NOT (external_id = ANY(?::text[]))`
		args = append(args, taskIDs)
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("close missing tasks: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit integration sync: %w", err)
	}
	return nil
}
