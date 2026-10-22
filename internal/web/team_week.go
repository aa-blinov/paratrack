package web

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// teamWeekView is the team's week on the statistics page (PLAN-04): a row per member,
// a column per day, and what each one tracked. A day that has passed, or is today, and
// falls under the daily norm is marked, so the manager sees it before the week ends.
type teamWeekView struct {
	Norm string
	Days []teamWeekDay
	Rows []teamWeekRow
}

type teamWeekDay struct {
	Label string
	Date  string
	Today bool
}

type teamWeekRow struct {
	Name  string
	Cells []teamWeekCell
	Total string
}

type teamWeekCell struct {
	Label string
	Below bool
	Mark  string // the day's overload or idle mark, as the page names it (TIME-13)
	Note  string
}

// teamWeekOf builds the week that holds today for the workspace. The week starts where
// the workspace starts its weeks (ADM-07), and the days are the viewer's own.
func (s *Server) teamWeekOf(r *http.Request, now time.Time) (*teamWeekView, error) {
	lang := resolveLang(r)
	today := startOfDay(now)
	start := startOfWeek(r, today)
	days := make([]time.Time, 7)
	view := &teamWeekView{Norm: fmtDur(r, dayNormSeconds), Rows: []teamWeekRow{}} // never null: the page reads it as a list
	for i := range days {
		days[i] = start.AddDate(0, 0, i)
		view.Days = append(view.Days, teamWeekDay{Label: fmtWeekday(lang, days[i]), Date: fmtDay(lang, days[i]), Today: days[i].Equal(today)})
	}
	tracked, err := s.services.Scheduling.TrackedSecondsByPersonDay(r.Context(), appmodel.PersonDayQuery{TeamID: teamID(r), Days: days, Now: now})
	if err != nil {
		return nil, err
	}
	marks, err := s.dayMarksOf(r, days)
	if err != nil {
		return nil, err
	}
	people := s.taskPeople(r)
	sort.SliceStable(people, func(i, j int) bool { return people[i].Name < people[j].Name })
	for _, person := range people {
		row := teamWeekRow{Name: person.Name}
		total := 0
		for i, day := range days {
			seconds := 0
			if values := tracked[person.ID]; values != nil {
				seconds = values[i]
			}
			total += seconds
			// A weekend with no time is a day off, not a shortfall.
			weekend := day.Weekday() == time.Saturday || day.Weekday() == time.Sunday
			below := !day.After(today) && seconds < dayNormSeconds && (!weekend || seconds > 0)
			cell := teamWeekCell{Label: cellLabel(r, seconds), Below: below}
			if mark, ok := marks[fmt.Sprintf("%d/%s", person.ID, day.Format("2006-01-02"))]; ok {
				cell.Mark = dayMarkLabel(lang, mark.Kind)
				cell.Note = mark.Note
			}
			row.Cells = append(row.Cells, cell)
		}
		row.Total = fmtDur(r, total)
		view.Rows = append(view.Rows, row)
	}
	return view, nil
}

// cellLabel shows a day's time, and nothing for a day with none.
func cellLabel(r *http.Request, seconds int) string {
	if seconds <= 0 {
		return ""
	}
	return fmtDur(r, seconds)
}

// dayMarksOf reads the workspace's marks for the days, keyed by member and day (TIME-13).
func (s *Server) dayMarksOf(r *http.Request, days []time.Time) (map[string]appmodel.DayMark, error) {
	marks, err := s.services.Scheduling.DayMarks(r.Context(), teamID(r),
		days[0].Format("2006-01-02"), days[len(days)-1].AddDate(0, 0, 1).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	out := make(map[string]appmodel.DayMark, len(marks))
	for _, mark := range marks {
		out[fmt.Sprintf("%d/%s", mark.UserID, mark.Day)] = mark
	}
	return out, nil
}

// dayMarkLabel names an overload or idle mark for the page.
func dayMarkLabel(lang i18n.Lang, kind string) string {
	if kind == "overload" {
		return i18n.T(lang, "stats.markOverload")
	}
	return i18n.T(lang, "stats.markIdle")
}

// markWeekView is the viewer's own side of the week (TIME-13): the days they can mark,
// and the marks they have set on them.
type markWeekView struct {
	Days  []markDay
	Marks []markView
}

type markDay struct {
	Value string // YYYY-MM-DD, what the form posts
	Label string
	Today bool
}

type markView struct {
	Day       string
	DayLabel  string
	Kind      string
	KindLabel string
	Note      string
}

// markWeekOf builds the week the viewer can mark, with their marks on it.
func (s *Server) markWeekOf(r *http.Request, now time.Time) (*markWeekView, error) {
	lang := resolveLang(r)
	today := startOfDay(now)
	start := startOfWeek(r, today)
	view := &markWeekView{Marks: []markView{}} // never null: the page reads it as a list
	days := make([]time.Time, 7)
	for i := range days {
		days[i] = start.AddDate(0, 0, i)
		view.Days = append(view.Days, markDay{
			Value: days[i].Format("2006-01-02"), Label: fmtWeekday(lang, days[i]) + " " + fmtDay(lang, days[i]), Today: days[i].Equal(today),
		})
	}
	marks, err := s.dayMarksOf(r, days)
	if err != nil {
		return nil, err
	}
	viewer := authenticatedUserID(r)
	for _, day := range days {
		mark, ok := marks[fmt.Sprintf("%d/%s", viewer, day.Format("2006-01-02"))]
		if !ok {
			continue
		}
		view.Marks = append(view.Marks, markView{
			Day: mark.Day, DayLabel: fmtWeekday(lang, day) + " " + fmtDay(lang, day),
			Kind: mark.Kind, KindLabel: dayMarkLabel(lang, mark.Kind), Note: mark.Note,
		})
	}
	return view, nil
}

// handleDayMark records or clears the viewer's own mark on a day (TIME-13). The form
// posts the day in the viewer's zone, the kind (overload, idle, or empty to clear) and an
// optional note of up to 200 characters.
func (s *Server) handleDayMark(w http.ResponseWriter, r *http.Request) {
	day, err := time.ParseInLocation("2006-01-02", r.FormValue("day"), userLoc(r))
	kind := r.FormValue("kind")
	note := strings.TrimSpace(r.FormValue("note"))
	if err != nil || (kind != "" && kind != "overload" && kind != "idle") || utf8.RuneCountInString(note) > 200 {
		http.Redirect(w, r, "/stats?flash=bad_request", http.StatusSeeOther)
		return
	}
	if err := s.services.Scheduling.SetDayMark(r.Context(), appmodel.DayMarkRequest{
		TeamID: teamID(r), UserID: authenticatedUserID(r), Day: day.Format("2006-01-02"), Kind: kind, Note: note, Now: userNow(r),
	}); err != nil {
		http.Redirect(w, r, "/stats?flash="+encodeFlash(false, s.teamErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/stats?flash=updated", http.StatusSeeOther)
}
