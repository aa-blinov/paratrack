package web

import (
	"encoding/json"
	"strings"
	"testing"
)

// mcpStudioID is the id of the shared workspace that studioOwnerAndMember makes.
func mcpStudioID(t *testing.T, owner *apiEnv) int64 {
	t.Helper()
	var id int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM teams WHERE name = 'Студия' ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("load the shared workspace: %v", err)
	}
	return id
}

// mcpMustCall calls a tool and fails the test when the call is refused.
func mcpMustCall(t *testing.T, e *apiEnv, auth map[string]string, name string, args map[string]any) string {
	t.Helper()
	text, failed := callTool(t, e, auth, name, args)
	if failed {
		t.Fatalf("%s: %s", name, text)
	}
	return text
}

// mcpID reads the id a write answered with.
func mcpID(t *testing.T, text string) int64 {
	t.Helper()
	var out struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil || out.ID == 0 {
		t.Fatalf("the answer has no id: %v %q", err, text)
	}
	return out.ID
}

// The owner creates a project, changes its rate and archives it. A field the call leaves
// out keeps its value.
func TestMCPOwnerCreatesAndUpdatesAProject(t *testing.T) {
	owner, _ := studioOwnerAndMember(t, "project")
	auth := mcpBearer(apiTokenOf(t, owner, "agent"))

	id := mcpID(t, mcpMustCall(t, owner, auth, "project_create", map[string]any{
		"name": "Сайт Ромашки", "billable_rate_cents": 500000, "currency": "RUB", "deadline": "2026-12-31",
	}))
	mcpMustCall(t, owner, auth, "project_update", map[string]any{"project_id": id, "name": "Сайт", "billable_rate_cents": 600000})

	var name, deadline string
	var rate int
	if err := owner.db.TestSQL().QueryRowContext(t.Context(),
		`SELECT name, billable_rate_cents, deadline FROM projects WHERE id = ?`, id).Scan(&name, &rate, &deadline); err != nil {
		t.Fatal(err)
	}
	if name != "Сайт" || rate != 600000 || deadline != "2026-12-31" {
		t.Fatalf("after project_update: name %q, rate %d, deadline %q", name, rate, deadline)
	}
	mcpMustCall(t, owner, auth, "project_archive", map[string]any{"project_id": id, "archived": true})
	if n := countRows(t, owner, `SELECT COUNT(*) FROM projects WHERE id = ? AND archived = 1`, id); n != 1 {
		t.Fatalf("archived projects = %d, want 1", n)
	}
}

// A member cannot create or change a project, or decide a time edit request. The refusal
// writes nothing.
func TestMCPMemberIsRefusedProjectWritesAndTheTimeEditDecision(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "project-refused")
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	refused := []struct {
		name string
		args map[string]any
	}{
		{"project_create", map[string]any{"name": "Чужой"}},
		{"project_update", map[string]any{"project_id": 1, "name": "Чужой"}},
		{"project_archive", map[string]any{"project_id": 1, "archived": true}},
		{"time_edit_request_decide", map[string]any{"request_id": 1, "approve": true}},
	}
	for _, tc := range refused {
		text, failed := callTool(t, member, memberAuth, tc.name, tc.args)
		if !failed || !strings.Contains(text, "прав") {
			t.Fatalf("a member called %s: failed=%v text=%q, want the forbidden text", tc.name, failed, text)
		}
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM projects WHERE name = 'Чужой'`); n != 0 {
		t.Fatalf("a refused project write was stored: %d project(s)", n)
	}
}

// A comment and a checklist step are written to a task, and the step can be ticked and removed.
func TestMCPTaskCommentAndChecklistAreWritten(t *testing.T) {
	owner, _ := studioOwnerAndMember(t, "task-feed")
	auth := mcpBearer(apiTokenOf(t, owner, "agent"))
	projectID := mcpID(t, mcpMustCall(t, owner, auth, "project_create", map[string]any{"name": "Лента"}))
	taskID := mcpID(t, mcpMustCall(t, owner, auth, "task_create", map[string]any{"project_id": projectID, "title": "Лендинг"}))

	mcpMustCall(t, owner, auth, "task_comment_add", map[string]any{"task_id": taskID, "body": "Макет согласован"})
	if n := countRows(t, owner, `SELECT COUNT(*) FROM task_comments WHERE task_id = ? AND body = 'Макет согласован'`, taskID); n != 1 {
		t.Fatalf("comments on the task = %d, want 1", n)
	}

	itemID := mcpID(t, mcpMustCall(t, owner, auth, "task_checklist_add", map[string]any{"task_id": taskID, "text": "Проверить верстку"}))
	mcpMustCall(t, owner, auth, "task_checklist_set_done", map[string]any{"task_id": taskID, "item_id": itemID, "done": true})
	if n := countRows(t, owner, `SELECT COUNT(*) FROM task_checklist_items WHERE id = ? AND body = 'Проверить верстку' AND done = 1`, itemID); n != 1 {
		t.Fatalf("the checklist step is not written as done")
	}
	mcpMustCall(t, owner, auth, "task_checklist_delete", map[string]any{"task_id": taskID, "item_id": itemID})
	if n := countRows(t, owner, `SELECT COUNT(*) FROM task_checklist_items WHERE id = ?`, itemID); n != 0 {
		t.Fatalf("the deleted checklist step is still there")
	}
}

// A task waits for another task of its project, and a task cannot wait for the one that
// waits for it. The wait can be removed.
func TestMCPTaskDependencyIsAddedAndRemoved(t *testing.T) {
	owner, _ := studioOwnerAndMember(t, "task-deps")
	auth := mcpBearer(apiTokenOf(t, owner, "agent"))
	projectID := mcpID(t, mcpMustCall(t, owner, auth, "project_create", map[string]any{"name": "Зависимости"}))
	first := mcpID(t, mcpMustCall(t, owner, auth, "task_create", map[string]any{"project_id": projectID, "title": "Макет"}))
	second := mcpID(t, mcpMustCall(t, owner, auth, "task_create", map[string]any{"project_id": projectID, "title": "Верстка"}))

	mcpMustCall(t, owner, auth, "task_dependency_add", map[string]any{"task_id": second, "depends_on_task_id": first})
	if n := countRows(t, owner, `SELECT COUNT(*) FROM task_dependencies WHERE task_id = ? AND depends_on_id = ?`, second, first); n != 1 {
		t.Fatalf("dependencies = %d, want 1", n)
	}
	text, failed := callTool(t, owner, auth, "task_dependency_add", map[string]any{"task_id": first, "depends_on_task_id": second})
	if !failed || !strings.Contains(text, "depends_on_task_id") {
		t.Fatalf("a cycle: failed=%v text=%q, want the field named", failed, text)
	}
	mcpMustCall(t, owner, auth, "task_dependency_remove", map[string]any{"task_id": second, "depends_on_task_id": first})
	if n := countRows(t, owner, `SELECT COUNT(*) FROM task_dependencies WHERE task_id = ? AND depends_on_id = ?`, second, first); n != 0 {
		t.Fatalf("dependencies after the removal = %d, want 0", n)
	}
}

// A session written after the fact shows its length, and an update that moves the end keeps
// the note the session already had.
func TestMCPSessionIsAddedAfterTheFactAndUpdated(t *testing.T) {
	owner, _ := studioOwnerAndMember(t, "session-edit")
	auth := mcpBearer(apiTokenOf(t, owner, "agent"))
	id := mcpID(t, mcpMustCall(t, owner, auth, "time_session_add", map[string]any{
		"activity": "Разработка", "start": "2026-09-21T09:00", "end": "2026-09-21T11:00", "note": "черновик",
	}))

	text := mcpMustCall(t, owner, auth, "time_session_update", map[string]any{"session_id": id, "end": "2026-09-21T12:00"})
	var view struct {
		Seconds int    `json:"seconds"`
		Note    string `json:"note"`
	}
	if err := json.Unmarshal([]byte(text), &view); err != nil {
		t.Fatalf("the answer is not a session: %v %q", err, text)
	}
	if view.Seconds != 3*3600 || view.Note != "черновик" {
		t.Fatalf("after the update: %d seconds, note %q; want 10800 seconds, note черновик", view.Seconds, view.Note)
	}
}

// A member asks to change one activity's time on one day, by the activity's name, and then by
// its id as the timesheet cell sends it. The owner approves the request. The day is then one
// timesheet session of the requested length, under the member.
func TestMCPMemberTimeEditRequestIsApprovedByTheOwner(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "time-edit")
	ownerAuth := mcpBearer(apiTokenOf(t, owner, "agent"))
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	mcpMustCall(t, member, memberAuth, "time_session_add", map[string]any{
		"activity": "Аналитика", "start": "2026-09-21T09:00", "end": "2026-09-21T10:00",
	})
	requestID := mcpID(t, mcpMustCall(t, member, memberAuth, "time_edit_request_create", map[string]any{
		"activity": "Аналитика", "day": "2026-09-21", "minutes": 90, "reason": "Таймер остался открытым",
	}))
	var activityID int64
	if err := member.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM activities WHERE name = 'Аналитика'`).Scan(&activityID); err != nil {
		t.Fatalf("load the activity: %v", err)
	}
	mcpMustCall(t, member, memberAuth, "time_edit_request_create", map[string]any{
		"activity_id": activityID, "day": "2026-09-21", "minutes": 60, "reason": "Так шлёт страница",
	})
	if text, failed := callTool(t, member, memberAuth, "time_edit_request_create", map[string]any{
		"activity_id": activityID, "day": "2026-13-01", "minutes": 90, "reason": "Другой день",
	}); !failed || !strings.Contains(text, "day") {
		t.Fatalf("an impossible day: failed=%v text=%q, want the field named", failed, text)
	}

	mcpMustCall(t, owner, ownerAuth, "time_edit_request_decide", map[string]any{"request_id": requestID, "approve": true, "note": "Принято"})
	if n := countRows(t, owner, `SELECT COUNT(*) FROM time_edit_requests WHERE id = ? AND status = 'approved'`, requestID); n != 1 {
		t.Fatalf("the request is not approved")
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM sessions
		WHERE user_id = (SELECT user_id FROM time_edit_requests WHERE id = ?) AND note = 'timesheet' AND accumulated_seconds = 5400`, requestID); n != 1 {
		t.Fatalf("the member's day does not hold the 90-minute session")
	}
}
