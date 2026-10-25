package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Calendar rules that live in the transaction (platform-crm-calendar-mcp, D3). Any
// member may create an event. Its author, a manager, and the participants see it;
// a shared event is seen by the whole workspace. Only the author or a manager
// changes or deletes it.

// rowsQuerier is the read side shared by the database handle and a transaction.
type rowsQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// participantsOf loads the user IDs listed on one event.
func participantsOf(ctx context.Context, q rowsQuerier, eventID int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT user_id FROM calendar_event_participants WHERE event_id = ? ORDER BY user_id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// checkParticipants confirms every listed user is a member of the workspace.
func checkParticipants(ctx context.Context, tx *Tx, teamID int64, userIDs []int64) error {
	for _, id := range userIDs {
		var ok int64
		err := tx.QueryRowContext(ctx, `SELECT user_id FROM memberships WHERE team_id = ? AND user_id = ?`, teamID, id).Scan(&ok)
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrCalendarTargetOutOfTeam
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// checkCalendarLinks confirms a linked project and client belong to the workspace.
func checkCalendarLinks(ctx context.Context, tx *Tx, teamID, projectID, clientID int64) error {
	if projectID > 0 {
		var id int64
		err := tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE id = ? AND team_id = ?`, projectID, teamID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return model.ErrCalendarTargetOutOfTeam
		}
		if err != nil {
			return err
		}
	}
	if clientID > 0 {
		if err := clientInTeam(ctx, tx, teamID, clientID); err != nil {
			return model.ErrCalendarTargetOutOfTeam
		}
	}
	return nil
}

func (d *DB) CreateCalendarEvent(ctx context.Context, request appmodel.CalendarEventCreateRequest) (model.CalendarEvent, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 {
		return model.CalendarEvent{}, ErrNotFound
	}
	title := strings.TrimSpace(request.Title)
	if title == "" {
		return model.CalendarEvent{}, fmt.Errorf("event title cannot be empty")
	}
	if !request.EndsAt.After(request.StartsAt) {
		return model.CalendarEvent{}, model.ErrEventEndBeforeStart
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CalendarEvent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, _, err := lockTeamRole(ctx, tx, request.TeamID, request.CallerID); err != nil {
		return model.CalendarEvent{}, err
	}
	if err := checkCalendarLinks(ctx, tx, request.TeamID, request.ProjectID, request.ClientID); err != nil {
		return model.CalendarEvent{}, err
	}
	if err := checkParticipants(ctx, tx, request.TeamID, request.ParticipantIDs); err != nil {
		return model.CalendarEvent{}, err
	}
	now := d.currentTime().UTC()
	shared := int64(0)
	if request.Shared {
		shared = 1
	}
	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO calendar_events (team_id, title, starts_at, ends_at, location, notes, project_id, client_id, shared, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, 0), NULLIF(?, 0), ?, ?, ?, ?) RETURNING id`,
		request.TeamID, title, FormatTime(request.StartsAt.UTC()), FormatTime(request.EndsAt.UTC()),
		strings.TrimSpace(request.Location), strings.TrimSpace(request.Notes), request.ProjectID, request.ClientID,
		shared, request.CallerID, FormatTime(now), FormatTime(now)).Scan(&id)
	if err != nil {
		return model.CalendarEvent{}, err
	}
	for _, uid := range uniqueIDs(request.ParticipantIDs) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO calendar_event_participants (event_id, user_id) VALUES (?, ?)`, id, uid); err != nil {
			return model.CalendarEvent{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.CalendarEvent{}, err
	}
	return d.calendarEventByID(ctx, request.TeamID, request.CallerID, id)
}

func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// calendarEventRow reads the event columns; the same projection serves every read.
const calendarEventColumns = `id, title, starts_at, ends_at, location, notes, COALESCE(project_id, 0), COALESCE(client_id, 0), shared, created_by, created_at, updated_at`

func scanCalendarEvent(teamID int64, row dealScanner) (model.CalendarEvent, error) {
	e := model.CalendarEvent{TeamID: teamID}
	var starts, ends, created, updated string
	var shared int64
	if err := row.Scan(&e.ID, &e.Title, &starts, &ends, &e.Location, &e.Notes, &e.ProjectID, &e.ClientID, &shared, &e.CreatedBy, &created, &updated); err != nil {
		return model.CalendarEvent{}, err
	}
	e.Shared = shared == 1
	var err error
	if e.StartsAt, err = ParseTime(starts); err != nil {
		return model.CalendarEvent{}, err
	}
	if e.EndsAt, err = ParseTime(ends); err != nil {
		return model.CalendarEvent{}, err
	}
	if e.CreatedAt, err = ParseTime(created); err != nil {
		return model.CalendarEvent{}, err
	}
	if e.UpdatedAt, err = ParseTime(updated); err != nil {
		return model.CalendarEvent{}, err
	}
	return e, nil
}

// visibleTo reports whether the caller may see the event.
func visibleTo(e model.CalendarEvent, callerID int64, manager bool) bool {
	if manager || e.Shared || e.CreatedBy == callerID {
		return true
	}
	for _, p := range e.Participants {
		if p == callerID {
			return true
		}
	}
	return false
}

func (d *DB) calendarEventByID(ctx context.Context, teamID, callerID, eventID int64) (model.CalendarEvent, error) {
	role, err := readTeamRole(ctx, d.sql, teamID, callerID)
	if err != nil {
		return model.CalendarEvent{}, err
	}
	e, err := scanCalendarEvent(teamID, d.sql.QueryRowContext(ctx,
		`SELECT `+calendarEventColumns+` FROM calendar_events WHERE id = ? AND team_id = ?`, eventID, teamID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.CalendarEvent{}, ErrNotFound
	}
	if err != nil {
		return model.CalendarEvent{}, err
	}
	if e.Participants, err = participantsOf(ctx, d.sql, e.ID); err != nil {
		return model.CalendarEvent{}, err
	}
	if !visibleTo(e, callerID, role.CanManage()) {
		return model.CalendarEvent{}, ErrNotFound
	}
	return e, nil
}

// calendarEventForEdit loads an event the caller may change: its author or a manager.
func calendarEventForEdit(ctx context.Context, tx *Tx, teamID, callerID, eventID int64) (model.CalendarEvent, error) {
	_, role, err := lockTeamRole(ctx, tx, teamID, callerID)
	if err != nil {
		return model.CalendarEvent{}, err
	}
	e, err := scanCalendarEvent(teamID, tx.QueryRowContext(ctx,
		`SELECT `+calendarEventColumns+` FROM calendar_events WHERE id = ? AND team_id = ?`, eventID, teamID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.CalendarEvent{}, ErrNotFound
	}
	if err != nil {
		return model.CalendarEvent{}, err
	}
	if !role.CanManage() && e.CreatedBy != callerID {
		return model.CalendarEvent{}, model.ErrForbidden
	}
	return e, nil
}

func (d *DB) UpdateCalendarEvent(ctx context.Context, request appmodel.CalendarEventUpdateRequest) (model.CalendarEvent, error) {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.EventID <= 0 {
		return model.CalendarEvent{}, ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return model.CalendarEvent{}, err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := calendarEventForEdit(ctx, tx, request.TeamID, request.CallerID, request.EventID)
	if err != nil {
		return model.CalendarEvent{}, err
	}
	starts, ends := current.StartsAt, current.EndsAt
	if request.StartsAt != nil {
		starts = *request.StartsAt
	}
	if request.EndsAt != nil {
		ends = *request.EndsAt
	}
	if !ends.After(starts) {
		return model.CalendarEvent{}, model.ErrEventEndBeforeStart
	}
	sets := []string{"updated_at = ?", "starts_at = ?", "ends_at = ?"}
	args := []any{FormatTime(d.currentTime().UTC()), FormatTime(starts.UTC()), FormatTime(ends.UTC())}
	if request.Title != nil {
		title := strings.TrimSpace(*request.Title)
		if title == "" {
			return model.CalendarEvent{}, fmt.Errorf("event title cannot be empty")
		}
		sets, args = append(sets, "title = ?"), append(args, title)
	}
	if request.Location != nil {
		sets, args = append(sets, "location = ?"), append(args, strings.TrimSpace(*request.Location))
	}
	if request.Notes != nil {
		sets, args = append(sets, "notes = ?"), append(args, strings.TrimSpace(*request.Notes))
	}
	projectID, clientID := int64(0), int64(0)
	if request.ProjectID != nil {
		projectID = *request.ProjectID
	} else {
		projectID = current.ProjectID
	}
	if request.ClientID != nil {
		clientID = *request.ClientID
	} else {
		clientID = current.ClientID
	}
	if err := checkCalendarLinks(ctx, tx, request.TeamID, projectID, clientID); err != nil {
		return model.CalendarEvent{}, err
	}
	sets, args = append(sets, "project_id = NULLIF(?, 0)", "client_id = NULLIF(?, 0)"), append(args, projectID, clientID)
	if request.Shared != nil {
		shared := int64(0)
		if *request.Shared {
			shared = 1
		}
		sets, args = append(sets, "shared = ?"), append(args, shared)
	}
	args = append(args, request.EventID, request.TeamID)
	if _, err := tx.ExecContext(ctx, `UPDATE calendar_events SET `+strings.Join(sets, ", ")+` WHERE id = ? AND team_id = ?`, args...); err != nil {
		return model.CalendarEvent{}, err
	}
	if request.ParticipantIDs != nil {
		if err := checkParticipants(ctx, tx, request.TeamID, request.ParticipantIDs); err != nil {
			return model.CalendarEvent{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM calendar_event_participants WHERE event_id = ?`, request.EventID); err != nil {
			return model.CalendarEvent{}, err
		}
		for _, uid := range uniqueIDs(request.ParticipantIDs) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO calendar_event_participants (event_id, user_id) VALUES (?, ?)`, request.EventID, uid); err != nil {
				return model.CalendarEvent{}, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return model.CalendarEvent{}, err
	}
	return d.calendarEventByID(ctx, request.TeamID, request.CallerID, request.EventID)
}

func (d *DB) DeleteCalendarEvent(ctx context.Context, request appmodel.CalendarEventDeleteRequest) error {
	if request.TeamID <= 0 || request.CallerID <= 0 || request.EventID <= 0 {
		return ErrNotFound
	}
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := calendarEventForEdit(ctx, tx, request.TeamID, request.CallerID, request.EventID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM calendar_events WHERE id = ? AND team_id = ?`, request.EventID, request.TeamID); err != nil {
		return err
	}
	return tx.Commit()
}

// ListCalendarEvents returns the events that overlap [From, To) and the caller may see.
func (d *DB) ListCalendarEvents(ctx context.Context, query appmodel.CalendarEventListQuery) ([]model.CalendarEvent, error) {
	if query.TeamID <= 0 || query.CallerID <= 0 {
		return nil, ErrNotFound
	}
	role, err := readTeamRole(ctx, d.sql, query.TeamID, query.CallerID)
	if err != nil {
		return nil, err
	}
	rows, err := d.sql.QueryContext(ctx,
		`SELECT `+calendarEventColumns+` FROM calendar_events
		  WHERE team_id = ? AND starts_at < ? AND ends_at > ? ORDER BY starts_at, id`,
		query.TeamID, FormatTime(query.To.UTC()), FormatTime(query.From.UTC()))
	if err != nil {
		return nil, err
	}
	var events []model.CalendarEvent
	for rows.Next() {
		e, err := scanCalendarEvent(query.TeamID, rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	out := make([]model.CalendarEvent, 0, len(events))
	for _, e := range events {
		if e.Participants, err = participantsOf(ctx, d.sql, e.ID); err != nil {
			return nil, err
		}
		if visibleTo(e, query.CallerID, role.CanManage()) {
			out = append(out, e)
		}
	}
	return out, nil
}
