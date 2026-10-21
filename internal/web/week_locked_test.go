package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

const weekLockedRu = "Неделя с 2 мар по 8 мар утверждена, правки закрыты. Попросите владельца или администратора снять утверждение."

// The store keeps the approved span as instants and its end is exclusive. The
// message has to name the first and the last day in the person's zone, not the
// instants themselves, or a week that starts late in the evening reads as the day before.
func TestWeekLockedMessageNamesTheFirstAndLastDayInThePersonsZone(t *testing.T) {
	msk := time.FixedZone("MSK", 3*60*60)
	lock := &model.SessionApprovalLockError{
		From: time.Date(2026, 10, 4, 21, 0, 0, 0, time.UTC),  // 5 October 00:00 in Moscow
		To:   time.Date(2026, 10, 11, 21, 0, 0, 0, time.UTC), // 12 October 00:00 in Moscow, exclusive
	}

	ru := "Неделя с 5 окт по 11 окт утверждена, правки закрыты. Попросите владельца или администратора снять утверждение."
	if got := weekLockedMessage(i18n.Ru, msk, lock); got != ru {
		t.Fatalf("ru message = %q\nwant       %q", got, ru)
	}
	en := "The week from Oct 5 to Oct 11 is approved and locked. Ask the owner or an admin to reopen it."
	if got := weekLockedMessage(i18n.En, msk, lock); got != en {
		t.Fatalf("en message = %q\nwant       %q", got, en)
	}
}

// Backfilling into an approved week names the week and the next step. The form
// shows that sentence, not the generic failure it showed before.
func TestBackfillIntoAnApprovedWeekNamesTheWeek(t *testing.T) {
	e := newAPIEnv(t)
	e.register("backfill-approved-week@x.test")
	approved := e.do("POST", "/api/timesheet/approve", url.Values{"start": {"2020-03-02"}, "end": {"2020-03-08"}}, nil)
	approved.Body.Close()

	body := readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"закрытая неделя"}, "start": {"2020-03-03T10:00"}, "end": {"2020-03-03T11:00"}, "note": {""},
	}, map[string]string{"HX-Request": "true"}))
	if !strings.Contains(body, weekLockedRu) {
		t.Fatalf("backfill answers %q, want the week named: %q", body, weekLockedRu)
	}
	if strings.Contains(body, "Что-то пошло не так") {
		t.Fatalf("backfill still answers the generic failure: %q", body)
	}
}

// A session saved before its week was approved cannot be moved inside that week.
// The editor and the v1 API refuse the move with the same sentence the backfill shows.
func TestSessionEditInAnApprovedWeekNamesTheWeek(t *testing.T) {
	e := newAPIEnv(t)
	e.register("edit-approved-week@x.test")
	readBody(t, e.do("POST", "/api/sessions/backfill", url.Values{
		"activity": {"правка закрытой недели"}, "start": {"2020-03-03T10:00"}, "end": {"2020-03-03T11:00"}, "note": {""},
	}, map[string]string{"HX-Request": "true"}))
	approved := e.do("POST", "/api/timesheet/approve", url.Values{"start": {"2020-03-02"}, "end": {"2020-03-08"}}, nil)
	approved.Body.Close()

	var id int64
	if err := e.db.TestSQL().QueryRowContext(context.Background(),
		`SELECT id FROM sessions ORDER BY id DESC LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("find the session: %v", err)
	}
	move := url.Values{"start_at": {"2020-03-03T09:00"}, "end_at": {"2020-03-03T10:30"}, "duration": {""}, "note": {""}}

	resp := e.do("PATCH", "/api/sessions/"+strconv.FormatInt(id, 10), move, map[string]string{"HX-Request": "true"})
	body := strings.TrimSpace(readBody(t, resp))
	if resp.StatusCode != http.StatusConflict || body != weekLockedRu {
		t.Fatalf("editor: %d %q, want 409 %q", resp.StatusCode, body, weekLockedRu)
	}

	resp = e.do("PATCH", "/api/v1/sessions/"+strconv.FormatInt(id, 10), move, nil)
	var answer map[string]string
	if err := json.Unmarshal([]byte(readBody(t, resp)), &answer); err != nil {
		t.Fatalf("v1 answer is not JSON: %v", err)
	}
	if resp.StatusCode != http.StatusConflict || answer["error"] != weekLockedRu {
		t.Fatalf("v1: %d %q, want 409 with the week named", resp.StatusCode, answer["error"])
	}
}
