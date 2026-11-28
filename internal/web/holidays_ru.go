package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// ruHoliday is a weekday off in the Russian production calendar, by name.
type ruHoliday struct {
	day, name string
}

// ruProductionCalendar holds the weekdays off of the Russian production
// calendar by year: the holidays of the Labour Code (article 112) and the
// transfers the government decree sets for that year (2026: Saturday 3
// January to Friday 9 January, Sunday 4 January to Thursday 31 December).
// Weekends need no entry: they are days off already. A year that is not here
// is not offered; a transferred working Saturday would go in as kind 'work'.
var ruProductionCalendar = map[int][]ruHoliday{
	2026: {
		{"2026-01-01", "Новогодние каникулы"}, {"2026-01-02", "Новогодние каникулы"},
		{"2026-01-05", "Новогодние каникулы"}, {"2026-01-06", "Новогодние каникулы"},
		{"2026-01-07", "Рождество Христово"}, {"2026-01-08", "Новогодние каникулы"},
		{"2026-01-09", "Перенос выходного с 3 января"},
		{"2026-02-23", "День защитника Отечества"},
		{"2026-03-09", "Международный женский день (перенос с 8 марта)"},
		{"2026-05-01", "Праздник Весны и Труда"},
		{"2026-05-11", "День Победы (перенос с 9 мая)"},
		{"2026-06-12", "День России"},
		{"2026-11-04", "День народного единства"},
		{"2026-12-31", "Перенос выходного с 4 января"},
	},
}

// handleHolidaysPreset fills the workspace's days off from the production
// calendar of a year, one entry per holiday with its name. Form: year, week.
// Days already marked keep what the manager set by hand.
func (s *Server) handleHolidaysPreset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	lang := resolveLang(r)
	back := absenceBack(r)
	year, _ := strconv.Atoi(strings.TrimSpace(r.PostForm.Get("year")))
	days, ok := ruProductionCalendar[year]
	if !ok {
		http.Redirect(w, r, back+"&flash="+encodeFlash(false, i18n.T(lang, "holiday.presetMissing")), http.StatusSeeOther)
		return
	}
	for _, day := range days {
		if err := s.services.Scheduling.SetTeamHolidays(r.Context(), appmodel.TeamHolidayRequest{TeamID: teamID(r), Days: []string{day.day}, Name: day.name}); err != nil {
			s.logInternalError(err)
			http.Redirect(w, r, back+"&flash="+encodeFlash(false, i18n.T(lang, "err.internal")), http.StatusSeeOther)
			return
		}
	}
	s.audit(r, "holiday.preset", strconv.Itoa(year), "ru")
	http.Redirect(w, r, back+"&flash="+encodeFlash(true, fmt.Sprintf(i18n.T(lang, "holiday.presetDone"), len(days), year)), http.StatusSeeOther)
}
