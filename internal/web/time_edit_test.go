package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

// A member asks for a change to one of their cells; only a manager decides, and an approval
// writes the cell as the member, so the time is theirs (TIME-19).
func TestTimeEditRequestIsApprovedByAManagerAndWritesTheCell(t *testing.T) {
	owner := newAPIEnv(t)
	owner.register("time-edit-owner@x.test")
	member := newAPIEnvSharedDB(t, owner)
	member.register("time-edit-member@x.test")
	joinStudio(t, owner, map[string]*apiEnv{"time-edit-member@x.test": member}, "time-edit-member@x.test")

	htmx := map[string]string{"HX-Request": "true"}
	readBody(t, member.do("POST", "/api/start", url.Values{"activity": {"сверстка"}}, htmx))
	var activityID, memberID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM activities WHERE name = 'сверстка'`).Scan(&activityID); err != nil {
		t.Fatal(err)
	}
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = 'time-edit-member@x.test'`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	created := member.do("POST", "/api/timesheet/edits", url.Values{
		"activity_id": {strconv.FormatInt(activityID, 10)}, "day": {day}, "minutes": {"90"}, "reason": {"забыл отметить созвон"},
	}, map[string]string{"Accept": "application/json"})
	body := readBody(t, created)
	if created.StatusCode != http.StatusOK {
		t.Fatalf("the request was refused: %d %s", created.StatusCode, body)
	}
	var requestID int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT id FROM time_edit_requests ORDER BY id DESC LIMIT 1`).Scan(&requestID); err != nil {
		t.Fatal(err)
	}
	decideURL := fmt.Sprintf("/api/timesheet/edits/%d/decision", requestID)

	mine := reactData[timesheetData](t, readBody(t, member.do("GET", "/timesheet", nil, nil)))
	if len(mine.EditRequests) != 1 || !mine.EditRequests[0].Mine || mine.EditRequests[0].Status != "pending" || mine.CanDecide {
		t.Fatalf("the member sees %+v, want their own pending request and no decision", mine.EditRequests)
	}

	refused := member.do("POST", decideURL, url.Values{"approve": {"1"}}, map[string]string{"Accept": "application/json"})
	readBody(t, refused)
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("a member approved their own request: %d, want 403", refused.StatusCode)
	}

	approved := owner.do("POST", decideURL, url.Values{"approve": {"1"}}, map[string]string{"Accept": "application/json"})
	readBody(t, approved)
	if approved.StatusCode != http.StatusOK {
		t.Fatalf("the manager could not approve: %d", approved.StatusCode)
	}
	var status string
	if err := owner.db.TestSQL().QueryRowContext(t.Context(), `SELECT status FROM time_edit_requests WHERE id = ?`, requestID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "approved" {
		t.Fatalf("the request is %q after the approval", status)
	}
	var seconds int64
	if err := owner.db.TestSQL().QueryRowContext(t.Context(),
		`SELECT COALESCE(SUM(accumulated_seconds), 0) FROM sessions WHERE user_id = ? AND activity_id = ? AND note = 'timesheet'`,
		memberID, activityID).Scan(&seconds); err != nil {
		t.Fatal(err)
	}
	if seconds != 90*60 {
		t.Fatalf("the cell holds %d seconds after the approval, want 5400 written as the member", seconds)
	}
}
