package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// An approval may not shrink a day below the time already logged with a start and an end: the
// request stays pending, the answer names the minutes field (MCP) or refuses with 422 (screen),
// and nothing is written (TIME-19).
func TestMCPApprovalBelowLoggedTimeIsRefused(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "time-edit-floor")
	ownerAuth := mcpBearer(apiTokenOf(t, owner, "agent"))
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))

	mcpMustCall(t, member, memberAuth, "time_session_add", map[string]any{
		"activity": "Аналитика", "start": "2026-09-22T09:00", "end": "2026-09-22T10:00",
	})
	requestID := mcpID(t, mcpMustCall(t, member, memberAuth, "time_edit_request_create", map[string]any{
		"activity": "Аналитика", "day": "2026-09-22", "minutes": 30, "reason": "Хочу меньше",
	}))

	text, failed := callTool(t, owner, ownerAuth, "time_edit_request_decide", map[string]any{"request_id": requestID, "approve": true})
	if !failed || !strings.Contains(text, "minutes") {
		t.Fatalf("an approval below the logged hour: failed=%v text=%q, want the minutes field named", failed, text)
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM time_edit_requests WHERE id = ? AND status = 'pending'`, requestID); n != 1 {
		t.Fatalf("the refused approval changed the request")
	}
	if n := countRows(t, owner, `SELECT COUNT(*) FROM sessions
		WHERE user_id = (SELECT user_id FROM time_edit_requests WHERE id = ?) AND note = 'timesheet'`, requestID); n != 0 {
		t.Fatalf("the refused approval wrote a timesheet session")
	}
}

// The same refusal on the timesheet screen: the decision answers 422 and the request stays pending.
func TestTimeEditApprovalBelowLoggedTimeAnswersUnprocessable(t *testing.T) {
	owner, member := studioOwnerAndMember(t, "time-edit-floor-web")
	memberAuth := mcpBearer(apiTokenOf(t, member, "member"))
	mcpMustCall(t, member, memberAuth, "time_session_add", map[string]any{
		"activity": "Аналитика", "start": "2026-09-23T09:00", "end": "2026-09-23T10:00",
	})
	var activityID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM activities WHERE name = 'Аналитика'`).Scan(&activityID); err != nil {
		t.Fatal(err)
	}

	created := member.do("POST", "/api/timesheet/edits", url.Values{
		"activity_id": {strconv.FormatInt(activityID, 10)}, "day": {"2026-09-23"}, "minutes": {"30"}, "reason": {"меньше"},
	}, map[string]string{"Accept": "application/json"})
	if body := readBody(t, created); created.StatusCode != http.StatusOK {
		t.Fatalf("the request was refused: %d %s", created.StatusCode, body)
	}
	var requestID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM time_edit_requests ORDER BY id DESC LIMIT 1`).Scan(&requestID); err != nil {
		t.Fatal(err)
	}

	refused := owner.do("POST", fmt.Sprintf("/api/timesheet/edits/%d/decision", requestID), url.Values{"approve": {"1"}},
		map[string]string{"Accept": "application/json"})
	if body := readBody(t, refused); refused.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("an approval below the logged hour: %d %s, want 422", refused.StatusCode, body)
	}
	var status string
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT status FROM time_edit_requests WHERE id = ?`, requestID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "pending" {
		t.Fatalf("the request is %q after the refused approval, want pending", status)
	}
}
