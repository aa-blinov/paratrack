package web

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// ruHoliday is a weekday off in the Russian production calendar, by name,
// or, with kind "short", a working day before a holiday that is an hour
// shorter (Labour Code article 95).
type ruHoliday struct {
	day, name string
	kind      string
}

// ruProductionCalendar holds the weekdays off of the Russian production
// calendar by year: the holidays of the Labour Code (article 112) and the
// transfers the government decree sets for that year (2026: Saturday 3
// January to Friday 9 January, Sunday 4 January to Thursday 31 December).
// Weekends need no entry: they are days off already. A year that is not here
// is not offered; a transferred working Saturday would go in as kind 'work'.
var ruProductionCalendar = map[int][]ruHoliday{
	2026: {
		{"2026-01-01", "Новогодние каникулы", "off"}, {"2026-01-02", "Новогодние каникулы", "off"},
		{"2026-01-05", "Новогодние каникулы", "off"}, {"2026-01-06", "Новогодние каникулы", "off"},
		{"2026-01-07", "Рождество Христово", "off"}, {"2026-01-08", "Новогодние каникулы", "off"},
		{"2026-01-09", "Перенос выходного с 3 января", "off"},
		{"2026-02-23", "День защитника Отечества", "off"},
		{"2026-03-09", "Международный женский день (перенос с 8 марта)", "off"},
		{"2026-05-01", "Праздник Весны и Труда", "off"},
		{"2026-05-11", "День Победы (перенос с 9 мая)", "off"},
		{"2026-06-12", "День России", "off"},
		{"2026-11-04", "День народного единства", "off"},
		{"2026-12-31", "Перенос выходного с 4 января", "off"},
		{"2026-04-30", "Предпраздничный день", "short"},
		{"2026-05-08", "Предпраздничный день", "short"},
		{"2026-06-11", "Предпраздничный день", "short"},
		{"2026-11-03", "Предпраздничный день", "short"},
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
	// One transaction for the year, so a failure leaves no half-filled
	// calendar; a day the manager already marked keeps its mark.
	request := appmodel.TeamHolidayRequest{TeamID: teamID(r), Names: map[string]string{}, Kinds: map[string]string{}, Keep: true}
	for _, day := range days {
		request.Days = append(request.Days, day.day)
		request.Names[day.day] = day.name
		request.Kinds[day.day] = day.kind
	}
	if err := s.services.Scheduling.SetTeamHolidays(r.Context(), request); err != nil {
		s.logInternalError(err)
		http.Redirect(w, r, back+"&flash="+encodeFlash(false, i18n.T(lang, "err.internal")), http.StatusSeeOther)
		return
	}
	s.audit(r, "holiday.preset", strconv.Itoa(year), "ru")
	http.Redirect(w, r, back+"&flash="+encodeFlash(true, fmt.Sprintf(i18n.T(lang, "holiday.presetDone"), len(days), year)), http.StatusSeeOther)
}

// ruCalendarYears are the years the calendar preset can fill, in order, for
// the timesheet's year picker.
func ruCalendarYears() []int {
	years := make([]int, 0, len(ruProductionCalendar))
	for year := range ruProductionCalendar {
		years = append(years, year)
	}
	sort.Ints(years)
	return years
}
