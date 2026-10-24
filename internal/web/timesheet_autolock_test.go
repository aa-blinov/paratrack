package web

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The refusal names the first open day and says who can change the term.
func TestAutoLockMessageNamesTheFirstOpenDay(t *testing.T) {
	cutoff := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	got := autoLockMessage(i18n.Ru, &model.SessionAutoLockError{Before: cutoff})
	want := "Правки до 17 окт закрыты настройкой пространства. Руководитель может изменить срок в настройках."
	if got != want {
		t.Fatalf("autoLockMessage = %q, want %q", got, want)
	}
}

// The closing term is saved from the calendar form, and a form that does not send
// it keeps the term the workspace already has, so an older page cannot switch the
// rule off by saving the zone.
func TestAutoLockTermIsSavedAndAnOldFormKeepsIt(t *testing.T) {
	e := newAPIEnv(t)
	e.register("autolock@x.test")

	saved := e.do("POST", "/api/team/calendar", url.Values{"timezone": {"Europe/Moscow"}, "week_start": {"mon"}, "autolock_days": {"7"}}, nil)
	readBody(t, saved)
	if loc := saved.Header.Get("Location"); !strings.Contains(loc, "flash=updated") {
		t.Fatalf("saving the term redirected to %q, want flash=updated", loc)
	}
	if page := readBody(t, e.do("GET", "/settings/team", nil, nil)); !strings.Contains(page, `"AutoLockDays":7`) {
		t.Fatal("the settings page does not carry the saved closing term of 7 days")
	}

	readBody(t, e.do("POST", "/api/team/calendar", url.Values{"timezone": {"Europe/Moscow"}, "week_start": {"mon"}}, nil))
	if page := readBody(t, e.do("GET", "/settings/team", nil, nil)); !strings.Contains(page, `"AutoLockDays":7`) {
		t.Fatal("a form without autolock_days switched the closing term off")
	}

	refused := e.do("POST", "/api/team/calendar", url.Values{"timezone": {"Europe/Moscow"}, "week_start": {"mon"}, "autolock_days": {"5"}}, nil)
	readBody(t, refused)
	if loc := refused.Header.Get("Location"); strings.Contains(loc, "flash=updated") {
		t.Fatalf("a term that is not offered was saved: %q", loc)
	}
	if page := readBody(t, e.do("GET", "/settings/team", nil, nil)); !strings.Contains(page, `"AutoLockDays":7`) {
		t.Fatal("a refused term changed the saved closing term")
	}
}

// With a term set, the timesheet says which past days are closed.
func TestTimesheetNamesTheClosedDaysWhenATermIsSet(t *testing.T) {
	e := newAPIEnv(t)
	e.register("autolock-sheet@x.test")
	readBody(t, e.do("POST", "/api/team/calendar", url.Values{"timezone": {"Europe/Moscow"}, "week_start": {"mon"}, "autolock_days": {"7"}}, nil))

	page := readBody(t, e.do("GET", "/timesheet", nil, nil))
	if !strings.Contains(page, "закрыты настройкой пространства") {
		t.Fatal("the timesheet does not say which past days are closed")
	}
}
