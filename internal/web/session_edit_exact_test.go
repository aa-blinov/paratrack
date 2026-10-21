package web

import (
	"database/sql"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
)

// A closed session that ran 1 h 30 m 20 s. The editor sends only the field that
// changed, and an edit must leave the stored seconds of the other fields alone.
func closedExactSession(t *testing.T, email string) (*apiEnv, string, time.Time, time.Time) {
	t.Helper()
	e := newAPIEnv(t)
	e.register(email)
	readBody(t, e.do("POST", "/api/start", url.Values{"activity": {"exact"}}, map[string]string{"HX-Request": "true"}))
	ids := activeSessionIDs(t, e)
	if len(ids) == 0 {
		t.Fatal("no running session after start")
	}
	id := strconv.FormatInt(ids[0], 10)
	resp := e.do("POST", "/api/sessions/"+id+"/stop", nil, nil)
	resp.Body.Close()

	start := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	end := start.Add(90*time.Minute + 20*time.Second)
	if _, err := e.db.TestSQL().ExecContext(t.Context(),
		`UPDATE sessions SET start_at = ?, end_at = ?, accumulated_seconds = ? WHERE id = ?`,
		db.FormatTime(start), db.FormatTime(end), 5420, id); err != nil {
		t.Fatal(err)
	}
	return e, id, start, end
}

// storedSession reads the session's raw times, its seconds and its note.
func storedSession(t *testing.T, e *apiEnv, id string) (startRaw, endRaw string, accumulated int64, note string) {
	t.Helper()
	var n sql.NullString
	if err := e.db.TestSQL().QueryRowContext(t.Context(),
		`SELECT start_at, end_at, accumulated_seconds, note FROM sessions WHERE id = ?`, id).
		Scan(&startRaw, &endRaw, &accumulated, &n); err != nil {
		t.Fatal(err)
	}
	return startRaw, endRaw, accumulated, n.String
}

func patchSession(t *testing.T, e *apiEnv, id string, form url.Values) {
	t.Helper()
	resp := e.do("PATCH", "/api/sessions/"+id, form, map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 204 {
		t.Fatalf("patch %v: %d %s", form, resp.StatusCode, readBody(t, resp))
	}
}

// A note edit writes the note and nothing else: the end keeps its seconds.
func TestSessionNoteEditLeavesTheExactEndAlone(t *testing.T) {
	e, id, _, _ := closedExactSession(t, "exact-note@x.test")
	startBefore, endBefore, accBefore, _ := storedSession(t, e, id)

	patchSession(t, e, id, url.Values{"note": {"kept"}})

	startAfter, endAfter, accAfter, note := storedSession(t, e, id)
	if note != "kept" {
		t.Fatalf("note = %q, want kept", note)
	}
	if startAfter != startBefore || endAfter != endBefore || accAfter != accBefore {
		t.Fatalf("note edit moved the session: start %s→%s, end %s→%s, seconds %d→%d",
			startBefore, startAfter, endBefore, endAfter, accBefore, accAfter)
	}
}

// A start edit keeps the end where it was and recomputes the length from the
// two, so the stored seconds stay exact.
func TestSessionStartEditKeepsTheEndAndRecomputesTheLength(t *testing.T) {
	e, id, _, _ := closedExactSession(t, "exact-start@x.test")
	startBefore, endBefore, _, _ := storedSession(t, e, id)
	start := mustParseStored(t, startBefore)
	patchSession(t, e, id, url.Values{"start_at": {start.Add(10 * time.Minute).Format("2006-01-02T15:04")}})

	startRaw, endAfter, acc, _ := storedSession(t, e, id)
	if endAfter != endBefore {
		t.Fatalf("start edit moved the end: %s→%s", endBefore, endAfter)
	}
	want := int64(mustParseStored(t, endAfter).Sub(mustParseStored(t, startRaw)) / time.Second)
	if acc != want || acc == 5420 {
		t.Fatalf("seconds = %d after start edit, want the span %d", acc, want)
	}
}

// An end edit recomputes the length from the start and the new end.
func TestSessionEndEditRecomputesTheLength(t *testing.T) {
	e, id, _, _ := closedExactSession(t, "exact-end@x.test")
	startBefore, _, _, _ := storedSession(t, e, id)
	start := mustParseStored(t, startBefore)

	patchSession(t, e, id, url.Values{"end_at": {start.Add(105 * time.Minute).Format("2006-01-02T15:04")}})

	startRaw, endRaw, acc, _ := storedSession(t, e, id)
	want := int64(mustParseStored(t, endRaw).Sub(mustParseStored(t, startRaw)) / time.Second)
	if acc != want || acc == 5420 {
		t.Fatalf("seconds = %d after end edit, want the span %d", acc, want)
	}
}

func mustParseStored(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := db.ParseTime(raw)
	if err != nil {
		t.Fatalf("parse stored time %q: %v", raw, err)
	}
	return parsed
}
