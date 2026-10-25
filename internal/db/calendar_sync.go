package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Calendar sync persistence (CRM-19). Each rule that decides whose rows a write may
// touch lives in the statement or the transaction here: a connection, its tokens and
// its links belong to one member of one workspace, and a sync never reads or changes
// another member's events. The tokens are sealed with the integration secret key, and
// the consent state keeps only hashes, so a token is never stored in the clear.

// calendarStateHash is the SHA-256 hex the integration secrets use; a consent state
// and a browser session are stored by it, never raw.
func calendarStateHash(raw string) string { return HashAPIToken(raw) }

const calendarConnectionColumns = `c.team_id, c.user_id, c.provider, c.connected_at, c.last_synced_at, c.last_error`

// calendarConnectionJoin keeps a connection visible only while its member is still in the workspace.
const calendarConnectionJoin = `calendar_connections c JOIN memberships m ON m.team_id = c.team_id AND m.user_id = c.user_id`

func scanCalendarConnection(row dealScanner) (appmodel.CalendarConnection, error) {
	var c appmodel.CalendarConnection
	var connected, synced string
	if err := row.Scan(&c.TeamID, &c.UserID, &c.Provider, &connected, &synced, &c.LastError); err != nil {
		return appmodel.CalendarConnection{}, err
	}
	var err error
	if c.ConnectedAt, err = ParseTime(connected); err != nil {
		return appmodel.CalendarConnection{}, err
	}
	if synced != "" {
		if c.LastSyncedAt, err = ParseTime(synced); err != nil {
			return appmodel.CalendarConnection{}, err
		}
	}
	return c, nil
}

// CalendarConnections lists a member's connections in one workspace, without tokens.
func (d *DB) CalendarConnections(ctx context.Context, teamID, userID int64) ([]appmodel.CalendarConnection, error) {
	if teamID <= 0 || userID <= 0 {
		return nil, model.ErrForbidden
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT `+calendarConnectionColumns+` FROM `+calendarConnectionJoin+`
	    WHERE c.team_id = ? AND c.user_id = ? ORDER BY c.provider`, teamID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.CalendarConnection
	for rows.Next() {
		c, err := scanCalendarConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DueCalendarConnections lists connections whose last sync attempt is before the cutoff,
// or that never synced. The scheduler decides the cadence; this only filters.
func (d *DB) DueCalendarConnections(ctx context.Context, before time.Time) ([]appmodel.CalendarConnection, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT `+calendarConnectionColumns+` FROM `+calendarConnectionJoin+`
	    WHERE c.last_synced_at = '' OR c.last_synced_at < ? ORDER BY c.id`, FormatTime(before))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.CalendarConnection
	for rows.Next() {
		c, err := scanCalendarConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CalendarCredentials returns a connection with its tokens for the sync. A connection
// whose member has left the workspace reads as not connected.
func (d *DB) CalendarCredentials(ctx context.Context, teamID, userID int64, provider string) (appmodel.CalendarCredentials, error) {
	row := d.sql.QueryRowContext(ctx, `SELECT `+calendarConnectionColumns+`, c.access_token, c.refresh_token, c.expires_at FROM `+calendarConnectionJoin+`
	    WHERE c.team_id = ? AND c.user_id = ? AND c.provider = ?`, teamID, userID, provider)
	var connected, synced, expires, access, refresh string
	var out appmodel.CalendarCredentials
	err := row.Scan(&out.Connection.TeamID, &out.Connection.UserID, &out.Connection.Provider, &connected, &synced, &out.Connection.LastError,
		&access, &refresh, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return appmodel.CalendarCredentials{}, model.ErrCalendarNotConnected
	}
	if err != nil {
		return appmodel.CalendarCredentials{}, err
	}
	if out.Connection.ConnectedAt, err = ParseTime(connected); err != nil {
		return appmodel.CalendarCredentials{}, err
	}
	if synced != "" {
		if out.Connection.LastSyncedAt, err = ParseTime(synced); err != nil {
			return appmodel.CalendarCredentials{}, err
		}
	}
	if out.Tokens.AccessToken, err = d.openSecret(access); err != nil {
		return appmodel.CalendarCredentials{}, fmt.Errorf("open calendar access token: %w", err)
	}
	if out.Tokens.RefreshToken, err = d.openSecret(refresh); err != nil {
		return appmodel.CalendarCredentials{}, fmt.Errorf("open calendar refresh token: %w", err)
	}
	if expires != "" {
		if out.Tokens.ExpiresAt, err = ParseTime(expires); err != nil {
			return appmodel.CalendarCredentials{}, err
		}
	}
	return out, nil
}

// SaveCalendarConnection stores the tokens of a connection, creating it on the first
// connect. An empty refresh token keeps the stored one, as a refresh may not return it.
func (d *DB) SaveCalendarConnection(ctx context.Context, request appmodel.CalendarConnectionSaveRequest) error {
	if _, err := readTeamRole(ctx, d.sql, request.TeamID, request.UserID); err != nil {
		return err
	}
	access, err := d.sealSecret(request.Tokens.AccessToken)
	if err != nil {
		return err
	}
	refresh, err := d.sealSecret(request.Tokens.RefreshToken)
	if err != nil {
		return err
	}
	expires := ""
	if !request.Tokens.ExpiresAt.IsZero() {
		expires = FormatTime(request.Tokens.ExpiresAt)
	}
	_, err = d.sql.ExecContext(ctx, `INSERT INTO calendar_connections (team_id, user_id, provider, access_token, refresh_token, expires_at, connected_at)
	    VALUES (?, ?, ?, ?, ?, ?, ?)
	    ON CONFLICT (team_id, user_id, provider) DO UPDATE SET
	      access_token = excluded.access_token,
	      refresh_token = CASE WHEN excluded.refresh_token = '' THEN calendar_connections.refresh_token ELSE excluded.refresh_token END,
	      expires_at = excluded.expires_at`,
		request.TeamID, request.UserID, request.Provider, access, refresh, expires, FormatTime(request.Now))
	return err
}

// RecordCalendarSync notes the outcome of one attempt. The error text is what the page shows.
func (d *DB) RecordCalendarSync(ctx context.Context, record appmodel.CalendarSyncRecordRequest) error {
	result, err := d.sql.ExecContext(ctx, `UPDATE calendar_connections SET last_synced_at = ?, last_error = ?
	    WHERE team_id = ? AND user_id = ? AND provider = ?`,
		FormatTime(record.At), record.Error, record.TeamID, record.UserID, record.Provider)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return model.ErrCalendarNotConnected
	}
	return nil
}

// DeleteCalendarConnection disconnects. It removes the tokens, the links and the
// tombstones of that connection. The events stay in the workspace as ordinary events:
// a pulled one becomes personal and unlinked, a pushed one stays at the provider.
func (d *DB) DeleteCalendarConnection(ctx context.Context, request appmodel.CalendarConnectionDeleteRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.UserID); err != nil {
		return err
	}
	teamID, userID, provider := request.TeamID, request.UserID, request.Provider
	if _, err := tx.ExecContext(ctx, `DELETE FROM calendar_external_links WHERE team_id = ? AND user_id = ? AND provider = ?`, teamID, userID, provider); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM calendar_external_tombstones WHERE team_id = ? AND user_id = ? AND provider = ?`, teamID, userID, provider); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM calendar_connections WHERE team_id = ? AND user_id = ? AND provider = ?`, teamID, userID, provider)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return model.ErrCalendarNotConnected
	}
	return tx.Commit()
}

// SaveCalendarOAuthState records a consent in progress. The verifier is sealed; the
// state and the session are kept as hashes. Expired rows are cleared on the way.
func (d *DB) SaveCalendarOAuthState(ctx context.Context, request appmodel.CalendarOAuthStateSaveRequest) error {
	if _, err := readTeamRole(ctx, d.sql, request.TeamID, request.UserID); err != nil {
		return err
	}
	verifier, err := d.sealSecret(request.Verifier)
	if err != nil {
		return err
	}
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM calendar_oauth_states WHERE expires_at < ?`, FormatTime(d.currentTime().UTC())); err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx, `INSERT INTO calendar_oauth_states (state_hash, session_hash, team_id, user_id, provider, verifier, expires_at)
	    VALUES (?, ?, ?, ?, ?, ?, ?)`,
		calendarStateHash(request.State), calendarStateHash(request.SessionToken), request.TeamID, request.UserID,
		request.Provider, verifier, FormatTime(request.ExpiresAt))
	return err
}

// TakeCalendarOAuthState consumes a consent. The row matches only with the state and the
// session that started it, and the delete makes the state single-use even under a replay.
func (d *DB) TakeCalendarOAuthState(ctx context.Context, request appmodel.CalendarOAuthStateTakeRequest) (appmodel.CalendarOAuthState, error) {
	if request.State == "" || request.SessionToken == "" {
		return appmodel.CalendarOAuthState{}, model.ErrNotFound
	}
	var out appmodel.CalendarOAuthState
	var sealed, expires string
	err := d.sql.QueryRowContext(ctx, `DELETE FROM calendar_oauth_states WHERE state_hash = ? AND session_hash = ?
	    RETURNING team_id, user_id, provider, verifier, expires_at`,
		calendarStateHash(request.State), calendarStateHash(request.SessionToken)).Scan(&out.TeamID, &out.UserID, &out.Provider, &sealed, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return appmodel.CalendarOAuthState{}, model.ErrNotFound
	}
	if err != nil {
		return appmodel.CalendarOAuthState{}, err
	}
	if out.Verifier, err = d.openSecret(sealed); err != nil {
		return appmodel.CalendarOAuthState{}, fmt.Errorf("open calendar verifier: %w", err)
	}
	if out.ExpiresAt, err = ParseTime(expires); err != nil {
		return appmodel.CalendarOAuthState{}, err
	}
	return out, nil
}

// CalendarLinks lists the links of one connection, with the team event each one joins.
func (d *DB) CalendarLinks(ctx context.Context, query appmodel.CalendarLinksQuery) ([]appmodel.CalendarLink, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT l.event_id, l.external_id, l.origin, l.etag, l.last_synced_at,
	    e.title, e.location, e.notes, e.starts_at, e.ends_at, e.updated_at
	    FROM calendar_external_links l JOIN calendar_events e ON e.id = l.event_id
	    WHERE l.team_id = ? AND l.user_id = ? AND l.provider = ? ORDER BY l.event_id`,
		query.TeamID, query.UserID, query.Provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.CalendarLink
	for rows.Next() {
		link, err := scanCalendarLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, link)
	}
	return out, rows.Err()
}

func scanCalendarLink(row dealScanner) (appmodel.CalendarLink, error) {
	var link appmodel.CalendarLink
	var synced, starts, ends, updated string
	if err := row.Scan(&link.EventID, &link.ExternalID, &link.Origin, &link.Etag, &synced,
		&link.Event.Title, &link.Event.Location, &link.Event.Notes, &starts, &ends, &updated); err != nil {
		return appmodel.CalendarLink{}, err
	}
	var err error
	if link.SyncedAt, err = ParseTime(synced); err != nil {
		return appmodel.CalendarLink{}, err
	}
	if link.Event.StartsAt, err = ParseTime(starts); err != nil {
		return appmodel.CalendarLink{}, err
	}
	if link.Event.EndsAt, err = ParseTime(ends); err != nil {
		return appmodel.CalendarLink{}, err
	}
	if link.UpdatedAt, err = ParseTime(updated); err != nil {
		return appmodel.CalendarLink{}, err
	}
	return link, nil
}

// CalendarPushCandidates lists the member's own events with push switched on that
// overlap the window. A pulled event is never a candidate: pushing it back would copy
// the provider's own event.
func (d *DB) CalendarPushCandidates(ctx context.Context, query appmodel.CalendarPushQuery) ([]appmodel.CalendarPushCandidate, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT e.id, e.title, e.location, e.notes, e.starts_at, e.ends_at, e.updated_at,
	    COALESCE(l.external_id, ''), COALESCE(l.etag, ''), COALESCE(l.last_synced_at, '')
	    FROM calendar_events e
	    LEFT JOIN calendar_external_links l ON l.event_id = e.id AND l.provider = ? AND l.origin = 'team'
	    WHERE e.team_id = ? AND e.created_by = ? AND e.sync_push = 1 AND e.starts_at < ? AND e.ends_at > ?
	      AND NOT EXISTS (SELECT 1 FROM calendar_external_links p WHERE p.event_id = e.id AND p.origin = 'provider')
	    ORDER BY e.id`,
		query.Provider, query.TeamID, query.UserID, FormatTime(query.To.UTC()), FormatTime(query.From.UTC()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.CalendarPushCandidate
	for rows.Next() {
		var c appmodel.CalendarPushCandidate
		var starts, ends, updated, externalID, etag, synced string
		if err := rows.Scan(&c.EventID, &c.Event.Title, &c.Event.Location, &c.Event.Notes, &starts, &ends, &updated,
			&externalID, &etag, &synced); err != nil {
			return nil, err
		}
		if c.Event.StartsAt, err = ParseTime(starts); err != nil {
			return nil, err
		}
		if c.Event.EndsAt, err = ParseTime(ends); err != nil {
			return nil, err
		}
		if c.UpdatedAt, err = ParseTime(updated); err != nil {
			return nil, err
		}
		if externalID != "" {
			c.Linked = true
			c.Link = appmodel.CalendarLink{EventID: c.EventID, ExternalID: externalID, Origin: appmodel.CalendarOriginTeam, Etag: etag}
			if c.Link.SyncedAt, err = ParseTime(synced); err != nil {
				return nil, err
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CalendarTombstones lists what the next sync must act on for one connection.
func (d *DB) CalendarTombstones(ctx context.Context, query appmodel.CalendarTombstoneRequest) ([]appmodel.CalendarTombstone, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT external_id, kind FROM calendar_external_tombstones
	    WHERE team_id = ? AND user_id = ? AND provider = ? ORDER BY external_id`,
		query.TeamID, query.UserID, query.Provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appmodel.CalendarTombstone
	for rows.Next() {
		var t appmodel.CalendarTombstone
		if err := rows.Scan(&t.ExternalID, &t.Kind); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteCalendarTombstone clears one handled tombstone.
func (d *DB) DeleteCalendarTombstone(ctx context.Context, query appmodel.CalendarTombstoneRequest) error {
	_, err := d.sql.ExecContext(ctx, `DELETE FROM calendar_external_tombstones
	    WHERE team_id = ? AND user_id = ? AND provider = ? AND external_id = ?`,
		query.TeamID, query.UserID, query.Provider, query.ExternalID)
	return err
}

// CreatePulledCalendarEvent brings a provider event in as the member's personal event,
// with its link, in one transaction.
func (d *DB) CreatePulledCalendarEvent(ctx context.Context, request appmodel.CalendarPulledEventRequest) (int64, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.UserID); err != nil {
		return 0, err
	}
	now := FormatTime(request.SyncedAt.UTC())
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO calendar_events (team_id, title, starts_at, ends_at, location, notes, shared, sync_push, created_by, created_at, updated_at)
	    VALUES (?, ?, ?, ?, ?, ?, 0, 0, ?, ?, ?) RETURNING id`,
		request.TeamID, request.Event.Title, FormatTime(request.Event.StartsAt.UTC()), FormatTime(request.Event.EndsAt.UTC()),
		request.Event.Location, request.Event.Notes, request.UserID, now, now).Scan(&id)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO calendar_external_links (event_id, team_id, user_id, provider, external_id, etag, origin, last_synced_at)
	    VALUES (?, ?, ?, ?, ?, ?, 'provider', ?)`,
		id, request.TeamID, request.UserID, request.Provider, request.ExternalID, request.Etag, now); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// requirePulledLink confirms the event is a pulled event of this connection. Every
// write to a pulled event goes through it, so a sync cannot touch another member's
// event or a pushed one.
func requirePulledLink(ctx context.Context, tx *Tx, teamID, userID, eventID int64, provider string) error {
	var one int64
	err := tx.QueryRowContext(ctx, `SELECT event_id FROM calendar_external_links
	    WHERE event_id = ? AND team_id = ? AND user_id = ? AND provider = ? AND origin = 'provider'`,
		eventID, teamID, userID, provider).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrNotFound
	}
	return err
}

// UpdatePulledCalendarEvent brings a changed provider event into the team. The provider's
// values replace the local ones.
func (d *DB) UpdatePulledCalendarEvent(ctx context.Context, request appmodel.CalendarPulledEventRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.UserID); err != nil {
		return err
	}
	if err := requirePulledLink(ctx, tx, request.TeamID, request.UserID, request.EventID, request.Provider); err != nil {
		return err
	}
	now := FormatTime(request.SyncedAt.UTC())
	if _, err := tx.ExecContext(ctx, `UPDATE calendar_events SET title = ?, location = ?, notes = ?, starts_at = ?, ends_at = ?, updated_at = ?
	    WHERE id = ? AND team_id = ?`,
		request.Event.Title, request.Event.Location, request.Event.Notes, FormatTime(request.Event.StartsAt.UTC()), FormatTime(request.Event.EndsAt.UTC()),
		now, request.EventID, request.TeamID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE calendar_external_links SET etag = ?, last_synced_at = ? WHERE event_id = ? AND provider = ?`,
		request.Etag, now, request.EventID, request.Provider); err != nil {
		return err
	}
	return tx.Commit()
}

// RemovePulledCalendarEvent deletes a pulled event the provider no longer holds. The
// link goes with it through the foreign key.
func (d *DB) RemovePulledCalendarEvent(ctx context.Context, request appmodel.CalendarSyncRemovalRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.UserID); err != nil {
		return err
	}
	if err := requirePulledLink(ctx, tx, request.TeamID, request.UserID, request.EventID, request.Provider); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM calendar_events WHERE id = ? AND team_id = ?`, request.EventID, request.TeamID); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveCalendarTeamLink records that a pushed event now exists at the provider. Only the
// author's own event with push switched on can be linked.
func (d *DB) SaveCalendarTeamLink(ctx context.Context, request appmodel.CalendarTeamLinkRequest) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.UserID); err != nil {
		return err
	}
	var one int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM calendar_events WHERE id = ? AND team_id = ? AND created_by = ? AND sync_push = 1`,
		request.EventID, request.TeamID, request.UserID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO calendar_external_links (event_id, team_id, user_id, provider, external_id, etag, origin, last_synced_at)
	    VALUES (?, ?, ?, ?, ?, ?, 'team', ?)
	    ON CONFLICT (event_id, provider) DO UPDATE SET external_id = excluded.external_id, etag = excluded.etag,
	      origin = 'team', last_synced_at = excluded.last_synced_at`,
		request.EventID, request.TeamID, request.UserID, request.Provider, request.ExternalID, request.Etag, FormatTime(request.SyncedAt.UTC())); err != nil {
		return err
	}
	return tx.Commit()
}
