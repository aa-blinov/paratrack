package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// A session can carry its own project: the column exists once the schema is
// applied, next to the activity's project it overrides.
func TestSessionProjectColumnIsMigrated(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	var n int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'sessions' AND column_name = 'project_id'`).Scan(&n); err != nil {
		t.Fatalf("read the sessions columns: %v", err)
	}
	if n != 1 {
		t.Fatalf("sessions has %d project_id columns, want 1", n)
	}
}

// A session that carries its own project counts under that project and under no
// other: the project totals, the spans, the project's session list, the invoice
// lines and the schedule's minutes all agree.
func TestEffectiveProjectCountsASessionUnderOneProject(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "effective")
	sql := d.TestSQL()
	var second int64
	if err := sql.QueryRowContext(ctx,
		`INSERT INTO projects (team_id, slug, name, color, created_at, updated_at)
		 VALUES (?, 'effective-second', 'Вторая', '#334455', ?, ?) RETURNING id`,
		ws.teamID, FormatTime(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), FormatTime(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))).Scan(&second); err != nil {
		t.Fatalf("create the second project: %v", err)
	}
	start := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	var sid int64
	if err := sql.QueryRowContext(ctx,
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds, created_at, updated_at, project_id)
		 VALUES (?, ?, ?, ?, ?, 3600, ?, ?, ?) RETURNING id`,
		ws.activityID, ws.teamID, ws.memberID, FormatTime(start), FormatTime(end), FormatTime(end), FormatTime(end), second).Scan(&sid); err != nil {
		t.Fatalf("insert the overriding session: %v", err)
	}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	through := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)

	if total, err := d.ProjectTrackedTotal(ctx, appmodel.ProjectScopeQuery{TeamID: ws.teamID, ProjectID: second}); err != nil || total != 3600 {
		t.Fatalf("the second project tracked %d (err %v), want the overriding hour only", total, err)
	}

	spans, err := d.ProjectSpans(ctx, appmodel.ProjectSpansQuery{TeamID: ws.teamID, From: from, Through: through})
	if err != nil {
		t.Fatalf("project spans: %v", err)
	}
	inSecond, inFirst := 0, 0
	for _, sp := range spans {
		if sp.Session.StartAt.Equal(start) {
			if sp.ProjectID == second {
				inSecond++
			} else {
				inFirst++
			}
		}
	}
	if inSecond != 1 || inFirst != 0 {
		t.Fatalf("the overriding session has %d spans under the second project and %d elsewhere, want 1 and 0", inSecond, inFirst)
	}

	listed, err := d.ProjectSessions(ctx, appmodel.ProjectActivityQuery{TeamID: ws.teamID, ProjectID: second, From: from, Through: through})
	if err != nil {
		t.Fatalf("project sessions: %v", err)
	}
	if len(listed) != 1 || listed[0].Session.ID != sid {
		t.Fatalf("the second project lists %d sessions, want only the overriding one", len(listed))
	}
	other, err := d.ProjectSessions(ctx, appmodel.ProjectActivityQuery{TeamID: ws.teamID, ProjectID: ws.projectID, From: from, Through: through})
	if err != nil {
		t.Fatalf("project sessions of the first project: %v", err)
	}
	for _, s := range other {
		if s.Session.ID == sid {
			t.Fatalf("the overriding session is also listed under the activity's project")
		}
	}

	lines, err := d.BuildInvoiceLines(ctx, ws.teamID, from, through, second)
	if err != nil {
		t.Fatalf("invoice lines of the second project: %v", err)
	}
	if !lineHoldsSession(lines, sid, second) {
		t.Fatalf("the second project's invoice lines do not hold the overriding session: %+v", lines)
	}
	firstLines, err := d.BuildInvoiceLines(ctx, ws.teamID, from, through, ws.projectID)
	if err != nil {
		t.Fatalf("invoice lines of the activity's project: %v", err)
	}
	for _, line := range firstLines {
		for _, id := range line.SessionIDs {
			if id == sid {
				t.Fatalf("the activity's project invoices the overriding session: %+v", line)
			}
		}
	}

	minutes, err := d.TrackedMinutesForProject(ctx, ws.teamID, second, through)
	if err != nil {
		t.Fatalf("schedule minutes of the second project: %v", err)
	}
	if len(minutes) != 1 || minutes[ws.memberID] != 60 {
		t.Fatalf("the second project's schedule minutes are %v, want the member's hour only", minutes)
	}
}

func lineHoldsSession(lines []model.InvoiceLine, sessionID, projectID int64) bool {
	for _, line := range lines {
		if line.ProjectID != projectID {
			continue
		}
		for _, id := range line.SessionIDs {
			if id == sessionID {
				return true
			}
		}
	}
	return false
}

// A start with a project leaves the activity and its history alone. The session
// takes the project when it differs from the activity's; a manager's project on an
// activity with no project and no sessions becomes the activity's, and nothing else.
func TestStartWithProjectOverridesOnlyTheNewSession(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "start-project")
	other := insertTestProject(t, d, ws.teamID, "start-project-other", "Другой")
	owner := requestctx.WithActor(ctx, ws.ownerID)
	member := requestctx.WithActor(ctx, ws.memberID)
	at := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

	start := func(ctx context.Context, activityID, projectID int64) model.Session {
		t.Helper()
		session, err := d.StartSession(ctx, appmodel.TimerStartRequest{TeamID: ws.teamID, ActivityID: activityID, At: at, ProjectID: projectID})
		if err != nil {
			t.Fatalf("start on activity %d with project %d: %v", activityID, projectID, err)
		}
		return session
	}

	// An activity with a project and history: the activity keeps its project, the
	// new session counts under the one chosen.
	withHistory := start(member, ws.activityID, other)
	if got := activityProjectOf(t, d, ws.activityID); got != ws.projectID {
		t.Fatalf("the activity with history moved to project %d, want %d", got, ws.projectID)
	}
	if got := sessionProjectOf(t, d, withHistory.ID); got != other {
		t.Fatalf("the new session has project %d, want %d", got, other)
	}

	// A manager's project on a new activity becomes the activity's; the session
	// takes its activity's project, so nothing is stored on it.
	newActivity := newTestActivity(t, d, owner, ws.teamID, ws.ownerID, "start-project-new")
	fresh := start(owner, newActivity, other)
	if got := activityProjectOf(t, d, newActivity); got != other {
		t.Fatalf("a manager's project on a new activity is %d, want %d", got, other)
	}
	if got := sessionProjectOf(t, d, fresh.ID); got != 0 {
		t.Fatalf("the session of a new activity stored project %d, want none", got)
	}

	// A member gives a new activity its first project, as before: the activity takes
	// it and the session inherits it. Only a later, different project would stay on
	// the session alone.
	memberActivity := newTestActivity(t, d, member, ws.teamID, ws.memberID, "start-project-member")
	byMember := start(member, memberActivity, other)
	if got := activityProjectOf(t, d, memberActivity); got != other {
		t.Fatalf("a member's first project is %d on the activity, want %d", got, other)
	}
	if got := sessionProjectOf(t, d, byMember.ID); got != 0 {
		t.Fatalf("the member's first-project session stored project %d, want none", got)
	}

	// The activity's own project is not repeated on the session.
	sameActivity := newTestActivity(t, d, owner, ws.teamID, ws.ownerID, "start-project-same")
	if err := d.AssignActivityProject(owner, appmodel.AssignActivityProjectRequest{TeamID: ws.teamID, ActivityID: sameActivity, ProjectID: ws.projectID, CallerID: ws.ownerID}); err != nil {
		t.Fatalf("assign the activity: %v", err)
	}
	same := start(member, sameActivity, ws.projectID)
	if got := sessionProjectOf(t, d, same.ID); got != 0 {
		t.Fatalf("a session with its activity's project stored %d, want none", got)
	}
}

// A past session added with a project follows the same rule as a start.
func TestClosedSessionWithProjectFollowsTheStartRule(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "closed-project")
	other := insertTestProject(t, d, ws.teamID, "closed-project-other", "Другой")
	owner := requestctx.WithActor(ctx, ws.ownerID)
	member := requestctx.WithActor(ctx, ws.memberID)
	start := time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	newActivity := newTestActivity(t, d, owner, ws.teamID, ws.ownerID, "closed-project-new")
	byOwner, err := d.CreateClosedSession(owner, appmodel.TimerAddRequest{TeamID: ws.teamID, ActivityID: newActivity, Start: start, End: end, ProjectID: other})
	if err != nil {
		t.Fatalf("add a past session as the manager: %v", err)
	}
	if got := activityProjectOf(t, d, newActivity); got != other {
		t.Fatalf("the manager's past session left the new activity at project %d, want %d", got, other)
	}
	if got := sessionProjectOf(t, d, byOwner.ID); got != 0 {
		t.Fatalf("the past session stored project %d, want none", got)
	}

	byMember, err := d.CreateClosedSession(member, appmodel.TimerAddRequest{TeamID: ws.teamID, ActivityID: ws.activityID, Start: start.Add(2 * time.Hour), End: end.Add(2 * time.Hour), ProjectID: other})
	if err != nil {
		t.Fatalf("add a past session as a member: %v", err)
	}
	if got := sessionProjectOf(t, d, byMember.ID); got != other {
		t.Fatalf("the member's past session has project %d, want %d", got, other)
	}
	if got := activityProjectOf(t, d, ws.activityID); got != ws.projectID {
		t.Fatalf("the activity with history moved to project %d, want %d", got, ws.projectID)
	}
}

func insertTestProject(t *testing.T, d *DB, teamID int64, slug, name string) int64 {
	t.Helper()
	at := FormatTime(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	var id int64
	if err := d.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO projects (team_id, slug, name, color, created_at, updated_at) VALUES (?, ?, ?, '#334455', ?, ?) RETURNING id`,
		teamID, slug, name, at, at).Scan(&id); err != nil {
		t.Fatalf("create project %s: %v", slug, err)
	}
	return id
}

func newTestActivity(t *testing.T, d *DB, ctx context.Context, teamID, callerID int64, name string) int64 {
	t.Helper()
	activity, err := d.GetOrCreateActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: callerID, Name: name})
	if err != nil {
		t.Fatalf("create activity %s: %v", name, err)
	}
	return activity.ID
}

// activityProjectOf and sessionProjectOf read the stored project columns; zero means none.
func activityProjectOf(t *testing.T, d *DB, activityID int64) int64 {
	t.Helper()
	var id int64
	if err := d.TestSQL().QueryRowContext(context.Background(),
		`SELECT COALESCE(project_id, 0) FROM activities WHERE id = ?`, activityID).Scan(&id); err != nil {
		t.Fatalf("read activity %d project: %v", activityID, err)
	}
	return id
}

func sessionProjectOf(t *testing.T, d *DB, sessionID int64) int64 {
	t.Helper()
	var id int64
	if err := d.TestSQL().QueryRowContext(context.Background(),
		`SELECT COALESCE(project_id, 0) FROM sessions WHERE id = ?`, sessionID).Scan(&id); err != nil {
		t.Fatalf("read session %d project: %v", sessionID, err)
	}
	return id
}

// Correcting one session's project changes that session alone: the other sessions
// and the activity keep their projects.
func TestCorrectingOneSessionsProjectTouchesOnlyThatSession(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "correct-one")
	other := insertTestProject(t, d, ws.teamID, "correct-one-other", "Другой")
	member := requestctx.WithActor(ctx, ws.memberID)
	at := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	mine := sessionFor(t, d, ws.teamID, ws.activityID, ws.memberID, at)
	neighbour := sessionFor(t, d, ws.teamID, ws.activityID, ws.memberID, at.Add(3*time.Hour))

	target := other
	if err := d.UpdateSessionFields(member, appmodel.SessionUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, SessionID: mine,
		Update: appmodel.SessionUpdate{ProjectID: &target, UpdatedAt: at},
	}); err != nil {
		t.Fatalf("correct the project of one session: %v", err)
	}
	if got := sessionProjectOf(t, d, mine); got != other {
		t.Fatalf("the corrected session has project %d, want %d", got, other)
	}
	if got := sessionProjectOf(t, d, neighbour); got != 0 {
		t.Fatalf("the neighbouring session moved to project %d", got)
	}
	if got := activityProjectOf(t, d, ws.activityID); got != ws.projectID {
		t.Fatalf("the activity moved to project %d", got)
	}

	// Zero returns the session to its activity's project.
	none := int64(0)
	if err := d.UpdateSessionFields(member, appmodel.SessionUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, SessionID: mine,
		Update: appmodel.SessionUpdate{ProjectID: &none, UpdatedAt: at},
	}); err != nil {
		t.Fatalf("return the session to its activity's project: %v", err)
	}
	if got := sessionProjectOf(t, d, mine); got != 0 {
		t.Fatalf("the session kept project %d after the reset", got)
	}
}

// A session that is part of an invoice keeps its project, whatever the invoice's
// status is.
func TestInvoicedSessionKeepsItsProject(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "invoiced-project")
	other := insertTestProject(t, d, ws.teamID, "invoiced-project-other", "Другой")
	owner := requestctx.WithActor(ctx, ws.ownerID)
	var invoiceID int64
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT invoice_id FROM sessions WHERE team_id = ? AND invoice_id IS NOT NULL ORDER BY id LIMIT 1`, ws.teamID).Scan(&invoiceID); err != nil {
		t.Fatalf("find an invoice of the workspace: %v", err)
	}
	billed := sessionFor(t, d, ws.teamID, ws.activityID, ws.memberID, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE sessions SET invoice_id = ? WHERE id = ?`, invoiceID, billed); err != nil {
		t.Fatalf("bill the session: %v", err)
	}
	target := other
	request := appmodel.SessionUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, SessionID: billed,
		Update: appmodel.SessionUpdate{ProjectID: &target, UpdatedAt: time.Now().UTC()},
	}
	before := sessionProjectOf(t, d, billed)

	// Draft: the time edit rules do not lock it, but the project must not move.
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE invoices SET status = 'draft' WHERE id = ?`, invoiceID); err != nil {
		t.Fatalf("make the invoice a draft: %v", err)
	}
	if err := d.UpdateSessionFields(owner, request); !errors.Is(err, model.ErrAlreadyBilled) {
		t.Fatalf("correct the project of a draft-invoiced session: %v, want ErrAlreadyBilled", err)
	}
	if got := sessionProjectOf(t, d, billed); got != before {
		t.Fatalf("the invoiced session moved from project %d to %d", before, got)
	}
}

func sessionFor(t *testing.T, d *DB, teamID, activityID, userID int64, start time.Time) int64 {
	t.Helper()
	var id int64
	if err := d.TestSQL().QueryRowContext(t.Context(),
		`INSERT INTO sessions (activity_id, team_id, user_id, start_at, end_at, accumulated_seconds, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 3600, ?, ?) RETURNING id`,
		activityID, teamID, userID, FormatTime(start), FormatTime(start.Add(time.Hour)),
		FormatTime(start), FormatTime(start)).Scan(&id); err != nil {
		t.Fatalf("insert a session: %v", err)
	}
	return id
}

// A session in an approved week keeps its project as well: the approval closes the
// week for every edit.
func TestApprovedWeekSessionKeepsItsProject(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "approved-project")
	other := insertTestProject(t, d, ws.teamID, "approved-project-other", "Другой")
	member := requestctx.WithActor(ctx, ws.memberID)
	var approvedStart string
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT starts_at FROM timesheet_approvals WHERE team_id = ? ORDER BY id LIMIT 1`, ws.teamID).Scan(&approvedStart); err != nil {
		t.Fatalf("find an approved week: %v", err)
	}
	approved, err := time.Parse("2006-01-02", approvedStart)
	if err != nil {
		t.Fatalf("parse the approved week start %q: %v", approvedStart, err)
	}
	inWeek := sessionFor(t, d, ws.teamID, ws.activityID, ws.memberID, approved.Add(time.Hour))
	before := sessionProjectOf(t, d, inWeek)
	target := other
	err = d.UpdateSessionFields(member, appmodel.SessionUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, SessionID: inWeek,
		Update: appmodel.SessionUpdate{ProjectID: &target, UpdatedAt: approved.Add(2 * time.Hour)},
	})
	if err == nil {
		t.Fatalf("a member corrected the project inside an approved week")
	}
	if got := sessionProjectOf(t, d, inWeek); got != before {
		t.Fatalf("the approved-week session moved from project %d to %d", before, got)
	}
}

// An edit writes one audit row per field it changed, with the value before and
// after, and no row for a field it left alone.
func TestSessionEditAuditsEachChangedField(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "audit-fields")
	member := requestctx.WithActor(ctx, ws.memberID)
	start := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	sid := sessionFor(t, d, ws.teamID, ws.activityID, ws.memberID, start)
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE sessions SET note = 'старая заметка' WHERE id = ?`, sid); err != nil {
		t.Fatalf("set the note: %v", err)
	}
	end := start.Add(30 * time.Minute)
	newNote := "новая заметка"
	if err := d.UpdateSessionFields(member, appmodel.SessionUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, SessionID: sid, IP: "10.0.0.7",
		Update: appmodel.SessionUpdate{EndAt: &end, Note: newNote, UpdatedAt: end},
	}); err != nil {
		t.Fatalf("edit the session: %v", err)
	}
	metas := auditMetas(t, d, sid)
	if !hasMetaPrefix(metas, "end_at: ") || !hasMetaPrefix(metas, "note: старая заметка -> новая заметка") {
		t.Fatalf("the audit rows are %q, want one for the end and one for the note", metas)
	}
	if hasMetaPrefix(metas, "start_at: ") {
		t.Fatalf("the start was not edited but the audit rows are %q", metas)
	}
	if ip := auditIP(t, d, sid); ip != "10.0.0.7" {
		t.Fatalf("the audit rows carry ip %q, want the request's", ip)
	}
}

// A rejected edit writes no audit row: the rows are part of the edit's transaction.
func TestRejectedSessionEditWritesNoAudit(t *testing.T) {
	ctx := t.Context()
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "audit-rejected")
	member := requestctx.WithActor(ctx, ws.memberID)
	var approvedStart string
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT starts_at FROM timesheet_approvals WHERE team_id = ? ORDER BY id LIMIT 1`, ws.teamID).Scan(&approvedStart); err != nil {
		t.Fatalf("find an approved week: %v", err)
	}
	approved, err := time.Parse("2006-01-02", approvedStart)
	if err != nil {
		t.Fatalf("parse the approved week start: %v", err)
	}
	sid := sessionFor(t, d, ws.teamID, ws.activityID, ws.memberID, approved.Add(time.Hour))
	end := approved.Add(90 * time.Minute)
	if err := d.UpdateSessionFields(member, appmodel.SessionUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, SessionID: sid,
		Update: appmodel.SessionUpdate{EndAt: &end, Note: "в закрытую неделю", UpdatedAt: end},
	}); err == nil {
		t.Fatalf("a member edited a session inside an approved week")
	}
	if metas := auditMetas(t, d, sid); len(metas) != 0 {
		t.Fatalf("the rejected edit left audit rows %q", metas)
	}
}

func auditMetas(t *testing.T, d *DB, sessionID int64) []string {
	t.Helper()
	rows, err := d.TestSQL().QueryContext(t.Context(),
		`SELECT meta FROM audit_log WHERE action = 'session.edit' AND target = ? ORDER BY id`, fmt.Sprintf("session:%d", sessionID))
	if err != nil {
		t.Fatalf("read audit rows: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var meta string
		if err := rows.Scan(&meta); err != nil {
			t.Fatalf("scan audit row: %v", err)
		}
		out = append(out, meta)
	}
	return out
}

func auditIP(t *testing.T, d *DB, sessionID int64) string {
	t.Helper()
	var ip string
	if err := d.TestSQL().QueryRowContext(t.Context(),
		`SELECT ip FROM audit_log WHERE action = 'session.edit' AND target = ? ORDER BY id LIMIT 1`, fmt.Sprintf("session:%d", sessionID)).Scan(&ip); err != nil {
		t.Fatalf("read the audit ip: %v", err)
	}
	return ip
}

func hasMetaPrefix(metas []string, prefix string) bool {
	for _, meta := range metas {
		if strings.HasPrefix(meta, prefix) {
			return true
		}
	}
	return false
}
