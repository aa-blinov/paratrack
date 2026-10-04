package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/importport"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/webhookport"
)

// ImportEntries persists one workspace import atomically. Locking the team
// serializes imports for that workspace, including duplicate checks and
// activity creation, while the unique external ID index remains a final guard.
func (d *DB) ImportEntries(ctx context.Context, request appmodel.ImportBatchRequest) (appmodel.ImportResult, error) {
	return d.importEntries(ctx, request.TeamID, request.CallerID, request.Provider, request.Entries)
}

func (d *DB) importEntries(ctx context.Context, teamID, callerID int64, provider string, entries []importport.ImportedEntry) (appmodel.ImportResult, error) {
	if teamID <= 0 || callerID <= 0 {
		return appmodel.ImportResult{}, model.ErrForbidden
	}
	if len(entries) > importport.MaxEntries {
		return appmodel.ImportResult{}, importport.ErrEntryLimit
	}
	result := appmodel.ImportResult{}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()

	if err := lockCurrentTeamMember(ctx, tx, teamID, callerID); err != nil {
		return result, fmt.Errorf("authorize import workspace: %w", err)
	}
	now := d.currentTime().UTC()
	existingIDs, err := findExistingImportIDs(ctx, tx, teamID, entries)
	if err != nil {
		return result, fmt.Errorf("find existing imported sessions: %w", err)
	}
	accepted := make([]importport.ImportedEntry, 0, len(entries))
	seenIDs := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.ExternalID != "" {
			if _, seen := seenIDs[entry.ExternalID]; seen {
				result.Skipped++
				continue
			}
			seenIDs[entry.ExternalID] = struct{}{}
			if _, exists := existingIDs[entry.ExternalID]; exists {
				result.Skipped++
				continue
			}
		}
		accepted = append(accepted, entry)
	}
	if len(accepted) > 0 {
		activityIDs, err := d.ensureImportActivities(ctx, tx, teamID, accepted, now)
		if err != nil {
			return result, fmt.Errorf("resolve imported activities: %w", err)
		}
		result.Imported, err = insertImportedSessions(ctx, tx, teamID, callerID, accepted, activityIDs)
		if err != nil {
			return result, fmt.Errorf("create imported sessions: %w", err)
		}
		result.Skipped += len(accepted) - result.Imported
	}
	if provider != "" {
		if err := d.recordWebhookEventTx(ctx, tx, teamID, webhookport.ImportCompletedEvent{Provider: provider, Imported: result.Imported}, now); err != nil {
			return appmodel.ImportResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return appmodel.ImportResult{}, fmt.Errorf("commit import: %w", err)
	}
	return result, nil
}

func findExistingImportIDs(ctx context.Context, tx *Tx, teamID int64, entries []importport.ImportedEntry) (map[string]struct{}, error) {
	ids := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.ExternalID == "" {
			continue
		}
		if _, exists := seen[entry.ExternalID]; exists {
			continue
		}
		seen[entry.ExternalID] = struct{}{}
		ids = append(ids, entry.ExternalID)
	}
	existing := make(map[string]struct{}, len(ids))
	if len(ids) == 0 {
		return existing, nil
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT external_id FROM sessions WHERE team_id = ? AND external_id = ANY(?::text[])`, teamID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		existing[id] = struct{}{}
	}
	return existing, rows.Err()
}

func (d *DB) ensureImportActivities(ctx context.Context, tx *Tx, teamID int64, entries []importport.ImportedEntry, now time.Time) (map[string]int64, error) {
	names := make([]string, 0, len(entries))
	keys := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Activity)
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
		keys = append(keys, key)
	}
	if len(names) == 0 {
		return map[string]int64{}, nil
	}
	createdAt := FormatTime(now.UTC())
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO activities (name, name_key, team_id, created_at, updated_at)
		SELECT source.name, source.name_key, ?, ?, ?
		FROM unnest(?::text[], ?::text[]) AS source(name, name_key)
		WHERE NOT EXISTS (
		  SELECT 1 FROM activities a WHERE a.team_id = ? AND a.name_key = source.name_key
		)`, teamID, createdAt, createdAt, names, keys, teamID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT DISTINCT ON (name_key) name_key, id FROM activities
		 WHERE team_id = ? AND name_key = ANY(?::text[]) ORDER BY name_key, id`, teamID, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	activityIDs := make(map[string]int64, len(keys))
	for rows.Next() {
		var key string
		var id int64
		if err := rows.Scan(&key, &id); err != nil {
			return nil, err
		}
		activityIDs[key] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return activityIDs, nil
}

func insertImportedSessions(ctx context.Context, tx *Tx, teamID, callerID int64, entries []importport.ImportedEntry, activityIDs map[string]int64) (int, error) {
	activityIDsBatch := make([]int64, 0, len(entries))
	starts, ends, notes, externalIDs := make([]string, 0, len(entries)), make([]string, 0, len(entries)), make([]string, 0, len(entries)), make([]string, 0, len(entries))
	seconds := make([]int64, 0, len(entries))
	for _, entry := range entries {
		name := strings.TrimSpace(entry.Activity)
		activityID, ok := activityIDs[strings.ToLower(name)]
		if !ok {
			return 0, fmt.Errorf("activity %q was not resolved", name)
		}
		activityIDsBatch = append(activityIDsBatch, activityID)
		starts = append(starts, FormatTime(entry.Start))
		ends = append(ends, FormatTime(entry.End))
		notes = append(notes, entry.Note)
		externalIDs = append(externalIDs, entry.ExternalID)
		seconds = append(seconds, int64(max(0, int(entry.End.Sub(entry.Start).Seconds()))))
	}
	rows, err := tx.QueryContext(ctx, `
		INSERT INTO sessions (activity_id, team_id, start_at, end_at, note, paused, accumulated_seconds, last_resume_at, user_id, external_id)
		SELECT source.activity_id, ?, source.start_at, source.end_at, NULLIF(source.note, ''), 0,
		       source.accumulated_seconds, NULL, ?, NULLIF(source.external_id, '')
		FROM unnest(?::bigint[], ?::text[], ?::text[], ?::text[], ?::bigint[], ?::text[])
		     AS source(activity_id, start_at, end_at, note, accumulated_seconds, external_id)
		ON CONFLICT (team_id, external_id) WHERE external_id IS NOT NULL DO NOTHING
		RETURNING id`, teamID, callerID, activityIDsBatch, starts, ends, notes, seconds, externalIDs)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	imported := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		imported++
	}
	return imported, rows.Err()
}
