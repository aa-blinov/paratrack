package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// absenceSpanLimit bounds one form: a vacation is weeks, not years, and a
// typo in a year should not write a thousand marks.
const absenceSpanLimit = 62

// absenceDays reads the from/to pair of an absence or holiday form, both
// YYYY-MM-DD in the viewer's location, as the list of days it covers.
func absenceDays(r *http.Request) ([]string, bool) {
	loc := userLoc(r)
	from, err1 := time.ParseInLocation("2006-01-02", strings.TrimSpace(r.PostForm.Get("from")), loc)
	to, err2 := time.ParseInLocation("2006-01-02", strings.TrimSpace(r.PostForm.Get("to")), loc)
	if err1 != nil {
		return nil, false
	}
	if err2 != nil {
		to = from
	}
	if to.Before(from) || to.Sub(from) > absenceSpanLimit*24*time.Hour {
		return nil, false
	}
	var days []string
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		days = append(days, d.Format("2006-01-02"))
	}
	return days, true
}

func absenceBack(r *http.Request) string {
	back := "/timesheet"
	if date := strings.TrimSpace(r.PostForm.Get("week")); date != "" {
		back += "?date=" + date
	} else {
		back += "?"
	}
	return back
}

// handleAbsence records a member's vacation, sick leave or day off over a
// span of days, or clears it with an empty kind. A manager does this for
// anyone; the days then leave the member's norm in the team summary.
func (s *Server) handleAbsence(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	lang := resolveLang(r)
	back := absenceBack(r)
	userID, err := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
	kind := strings.TrimSpace(r.PostForm.Get("kind"))
	days, ok := absenceDays(r)
	if err != nil || userID <= 0 || !ok || (kind != "" && !absenceKinds[kind]) {
		http.Redirect(w, r, back+"&flash="+encodeFlash(false, i18n.T(lang, "absence.bad")), http.StatusSeeOther)
		return
	}
	for _, day := range days {
		if err := s.services.Scheduling.SetDayMark(r.Context(), appmodel.DayMarkRequest{
			TeamID: teamID(r), UserID: userID, Day: day, Kind: kind, Note: strings.TrimSpace(r.PostForm.Get("note")), Now: userNow(r),
		}); err != nil {
			http.Redirect(w, r, back+"&flash="+encodeFlash(false, i18n.T(lang, "absence.bad")), http.StatusSeeOther)
			return
		}
	}
	s.audit(r, "absence.set", strconv.FormatInt(userID, 10), fmt.Sprintf("%s %s..%s", kind, days[0], days[len(days)-1]))
	key := "absence.saved"
	if kind == "" {
		key = "absence.cleared"
	}
	http.Redirect(w, r, back+"&flash="+encodeFlash(true, fmt.Sprintf(i18n.T(lang, key), len(days))), http.StatusSeeOther)
}

// handleHoliday marks the workspace's days off over a span, or clears them.
func (s *Server) handleHoliday(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	lang := resolveLang(r)
	back := absenceBack(r)
	days, ok := absenceDays(r)
	if !ok {
		http.Redirect(w, r, back+"&flash="+encodeFlash(false, i18n.T(lang, "absence.bad")), http.StatusSeeOther)
		return
	}
	clear := r.PostForm.Get("clear") != ""
	name := strings.TrimSpace(r.PostForm.Get("name"))
	for _, day := range days {
		if err := s.services.Scheduling.SetTeamHoliday(r.Context(), appmodel.TeamHolidayRequest{TeamID: teamID(r), Day: day, Name: name, Clear: clear}); err != nil {
			s.writeInternalError(w, err)
			return
		}
	}
	s.audit(r, "holiday.set", days[0]+".."+days[len(days)-1], name)
	key := "holiday.saved"
	if clear {
		key = "holiday.cleared"
	}
	http.Redirect(w, r, back+"&flash="+encodeFlash(true, fmt.Sprintf(i18n.T(lang, key), len(days))), http.StatusSeeOther)
}
