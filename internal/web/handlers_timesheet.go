package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// ---------------------------------------------------------------------------
// Timesheet — week grid (activity × Mon..Sun)
// ---------------------------------------------------------------------------

type timesheetDay struct {
	Index   int    // 0..6
	Label   string // "Mon"
	Date    string // "22"
	ISO     string // 2026-09-22
	Secs    int
	Min     int    // minutes for the cell input
	Total   string // formatted
	IsToday bool
}

type timesheetRow struct {
	ActivityID    int64
	ActivityName  string
	ProjectID     int64
	Color         string
	Secs          [7]int
	Cells         [7]timesheetDay // copy of day headers + this row's secs
	RowTotal      int
	RowTotalLabel string
}

func (timesheetRow) isTemplateData()         {}
func (timesheetRow) isFragmentTemplateData() {}

type timesheetData struct {
	pageData
	TimesheetReact  bool
	WeekStart       time.Time
	WeekEnd         time.Time
	PrevWeek        string // link query
	NextWeek        string
	WeekLabel       string // "Sep 22 – Sep 28"
	Days            []timesheetDay
	Rows            []timesheetRow
	ProjectNames    map[int64]string
	DayTotals       [7]int
	DayTotalLabels  [7]string
	GrandTotal      int
	GrandTotalLabel string
	Others          []activityView // big workspace: activities not on the sheet
	Added           []int64        // rows added by hand this visit
	DateISO         string
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

// handleTimesheet renders the weekly grid. ?date= any day inside the
// week selects it; default is today's week.
func (s *Server) handleTimesheet(w http.ResponseWriter, r *http.Request) {
	now := userNow(r)
	day := now
	if v := r.URL.Query().Get("date"); v != "" {
		if t, err := timeparse.ParseDateTime(v, now); err == nil {
			day = t
		}
	}
	weekStart := startOfWeek(r, day)
	weekEnd := weekStart.AddDate(0, 0, 6)
	lang := string(resolveLang(r))

	var added []int64
	for _, v := range r.URL.Query()["add"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
			added = append(added, id)
		}
	}
	grid, err := s.services.Tracking.Queries.Timesheet(r.Context(), appmodel.TimesheetRequest{
		TeamID: teamID(r), WeekStart: weekStart, Now: now, ExtraActivityIDs: added,
	})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}

	projects, err := s.services.Projects.Queries.List(r.Context(), appmodel.ProjectCatalogQuery{TeamID: teamID(r), IncludeArchived: true})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	projectNames := make(map[int64]string, len(projects))
	for _, project := range projects {
		projectNames[project.ID] = project.Name
	}
	days := make([]timesheetDay, 7)
	for i := 0; i < 7; i++ {
		d := weekStart.AddDate(0, 0, i)
		days[i] = timesheetDay{
			Index:   i,
			Label:   fmtWeekday(resolveLang(r), d),
			Date:    d.Format("2"),
			ISO:     d.Format("2006-01-02"),
			Secs:    grid.DayTotals[i],
			Min:     grid.DayTotals[i] / 60,
			Total:   fmtDur(r, grid.DayTotals[i]),
			IsToday: sameDay(d, now),
		}
	}

	rows := make([]timesheetRow, 0, len(grid.Rows))
	for _, rc := range grid.Rows {
		row := timesheetRow{
			ActivityID:    rc.ActivityID,
			ActivityName:  rc.ActivityName,
			ProjectID:     rc.ProjectID,
			Color:         colorFor(rc.ActivityName),
			Secs:          rc.Secs,
			RowTotal:      rc.RowTotal,
			RowTotalLabel: fmtDur(r, rc.RowTotal),
		}
		for i := 0; i < 7; i++ {
			row.Cells[i] = timesheetDay{
				Index:   i,
				ISO:     days[i].ISO,
				Secs:    rc.Secs[i],
				Min:     cellMin(rc.Secs[i]),
				Total:   fmtDur(r, rc.Secs[i]),
				IsToday: days[i].IsToday,
			}
		}
		rows = append(rows, row)
	}
	var dayTotalLabels [7]string
	for i := 0; i < 7; i++ {
		dayTotalLabels[i] = fmtDur(r, grid.DayTotals[i])
	}

	data := timesheetData{
		pageData: pageData{
			Title: "Timesheet", Active: "timesheet", Lang: lang, ReactApp: true,
		},
		TimesheetReact:  true,
		WeekStart:       weekStart,
		WeekEnd:         weekEnd,
		WeekLabel:       fmtDay(resolveLang(r), weekStart) + " – " + fmtDay(resolveLang(r), weekEnd),
		PrevWeek:        weekStart.AddDate(0, 0, -7).Format("2006-01-02"),
		NextWeek:        weekStart.AddDate(0, 0, 7).Format("2006-01-02"),
		Days:            days,
		Rows:            rows,
		ProjectNames:    projectNames,
		DayTotals:       grid.DayTotals,
		DayTotalLabels:  dayTotalLabels,
		GrandTotal:      grid.GrandTotal,
		GrandTotalLabel: fmtDur(r, grid.GrandTotal),
		Others:          activityViews(grid.Others, string(resolveLang(r))),
		Added:           added,
		DateISO:         weekStart.Format("2006-01-02"),
	}
	s.renderPageForRequest(w, r, "Timesheet", "timesheet", "timesheet", &data)
}

// handleTimesheetCell writes one grid cell. Form:
//
//	activity_id, date (YYYY-MM-DD), minutes
//
// Responds with the re-rendered row so HTMX can swap it.
func (s *Server) handleTimesheetCell(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	actID, err := strconv.ParseInt(r.FormValue("activity_id"), 10, 64)
	if err != nil || actID <= 0 {
		http.Error(w, "activity_id required", 400)
		return
	}
	dateStr := strings.TrimSpace(r.FormValue("date"))
	day, err := time.ParseInLocation("2006-01-02", dateStr, userLoc(r))
	if err != nil {
		http.Error(w, "bad date", 400)
		return
	}
	mins, err := strconv.Atoi(strings.TrimSpace(r.FormValue("minutes")))
	if err != nil || mins < 0 || mins > 24*60 {
		http.Error(w, "minutes must be 0..1440", 400)
		return
	}
	if err := s.services.Tracking.Commands.SetDayTotal(r.Context(), appmodel.TimesheetCellUpdateRequest{TeamID: teamID(r), ActivityID: actID, Day: day, TotalSeconds: mins * 60}); err != nil {
		s.respondTimesheetWriteError(w, r, actID, day, err)
		return
	}
	s.toastL(w, r, "toast.saved", "", "success")
	// Re-render just the row.
	s.respondTimesheetRow(w, r, actID, day)
}

// handleTimesheetRowClear empties one activity row for the whole week that is
// on screen. Form:
//
//	activity_id, date (any day inside the week)
//
// It is the row-level twin of handleTimesheetCell: a wrongly added row is
// cleared in one request and one transaction instead of seven cell edits, and
// the response is the same recomputed row so day and week totals stay right.
func (s *Server) handleTimesheetRowClear(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	actID, err := strconv.ParseInt(r.FormValue("activity_id"), 10, 64)
	if err != nil || actID <= 0 {
		http.Error(w, "activity_id required", 400)
		return
	}
	dateStr := strings.TrimSpace(r.FormValue("date"))
	day, err := time.ParseInLocation("2006-01-02", dateStr, userLoc(r))
	if err != nil {
		http.Error(w, "bad date", 400)
		return
	}
	weekStart := startOfWeek(r, day)
	if err := s.services.Tracking.Commands.ClearRow(r.Context(), appmodel.TimesheetRowClearRequest{TeamID: teamID(r), ActivityID: actID, WeekStart: weekStart}); err != nil {
		s.respondTimesheetWriteError(w, r, actID, weekStart, err)
		return
	}
	s.toastL(w, r, "toast.saved", "", "success")
	s.respondTimesheetRow(w, r, actID, weekStart)
}

// respondTimesheetWriteError maps a failed grid write onto the transport the
// caller used: JSON clients get a status or a message field, the legacy HTMX
// form gets a toast plus the re-rendered row carrying the same message. Billed
// time is reported as a refusal, not as a generic failure.
func (s *Server) respondTimesheetWriteError(w http.ResponseWriter, r *http.Request, actID int64, day time.Time, err error) {
	var lockErr *model.SessionInvoiceLockError
	if errors.As(err, &lockErr) {
		// Put the row back to what the invoice billed.
		message := fmt.Sprintf(i18n.T(resolveLang(r), "inv.locked"), lockErr.InvoiceNumber)
		if !wantsJSON(r) {
			s.toast(w, message, "error")
		}
		s.respondTimesheetRow(w, r, actID, day, message)
		return
	}
	if errors.Is(err, appmodel.ErrInvalidSessionEdit) {
		if wantsJSON(r) {
			http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
			return
		}
		s.toastL(w, r, "err.invalidInput", "", "error")
	} else {
		if wantsJSON(r) {
			s.writeInternalError(w, err)
			return
		}
		s.logInternalError(err)
		s.toastL(w, r, "err.internal", "", "error")
	}
	w.WriteHeader(200)
}

type timesheetCellJSON struct {
	ISO   string `json:"iso"`
	Secs  int    `json:"secs"`
	Min   int    `json:"min"`
	Total string `json:"total"`
}

type timesheetDayTotalJSON struct {
	Secs  int    `json:"secs"`
	Total string `json:"total"`
}

type timesheetRowJSON struct {
	ActivityID      int64                   `json:"activityId"`
	ActivityName    string                  `json:"activityName"`
	Color           string                  `json:"color"`
	Cells           []timesheetCellJSON     `json:"cells"`
	RowTotal        int                     `json:"rowTotal"`
	RowTotalLabel   string                  `json:"rowTotalLabel"`
	DayTotals       []timesheetDayTotalJSON `json:"dayTotals"`
	GrandTotal      int                     `json:"grandTotal"`
	GrandTotalLabel string                  `json:"grandTotalLabel"`
	Error           string                  `json:"error,omitempty"`
}

func (timesheetRowJSON) isJSONResponse() {}

func (s *Server) respondTimesheetRow(w http.ResponseWriter, r *http.Request, actID int64, day time.Time, failure ...string) {
	weekStart := startOfWeek(r, day)
	now := userNow(r)
	grid, err := s.services.Tracking.Queries.Timesheet(r.Context(), appmodel.TimesheetRequest{
		TeamID: teamID(r), WeekStart: weekStart, Now: now, ExtraActivityIDs: []int64{actID},
	})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	for _, rc := range grid.Rows {
		if rc.ActivityID != actID {
			continue
		}
		row := timesheetRow{
			ActivityID: rc.ActivityID, ActivityName: rc.ActivityName,
			Color: colorFor(rc.ActivityName), Secs: rc.Secs,
			RowTotal: rc.RowTotal, RowTotalLabel: fmtDur(r, rc.RowTotal),
		}
		for i := 0; i < 7; i++ {
			d := weekStart.AddDate(0, 0, i)
			row.Cells[i] = timesheetDay{
				Index: i, ISO: d.Format("2006-01-02"),
				Secs: rc.Secs[i], Min: cellMin(rc.Secs[i]), Total: fmtDur(r, rc.Secs[i]),
				IsToday: sameDay(d, now),
			}
		}
		if wantsJSON(r) {
			response := timesheetRowJSON{
				ActivityID: rc.ActivityID, ActivityName: rc.ActivityName, Color: colorFor(rc.ActivityName),
				Cells: make([]timesheetCellJSON, 0, 7), RowTotal: rc.RowTotal, RowTotalLabel: fmtDur(r, rc.RowTotal),
				DayTotals: make([]timesheetDayTotalJSON, 0, 7), GrandTotal: grid.GrandTotal, GrandTotalLabel: fmtDur(r, grid.GrandTotal),
			}
			for i := 0; i < 7; i++ {
				d := weekStart.AddDate(0, 0, i)
				response.Cells = append(response.Cells, timesheetCellJSON{ISO: d.Format("2006-01-02"), Secs: rc.Secs[i], Min: cellMin(rc.Secs[i]), Total: fmtDur(r, rc.Secs[i])})
				response.DayTotals = append(response.DayTotals, timesheetDayTotalJSON{Secs: grid.DayTotals[i], Total: fmtDur(r, grid.DayTotals[i])})
			}
			if len(failure) > 0 {
				response.Error = failure[0]
			}
			s.writeJSON(w, response)
			return
		}
		s.renderFragment(w, "timesheet-row", row)
		return
	}
	// Activity vanished from the grid — render an empty row shell.
	http.Error(w, "row not found", 404)
}

// startOfWeek returns 00:00 of the first day of t's week: Monday, or
// Sunday when the user picked that in their preferences.
// cellMin rounds a cell to whole minutes, never showing 0 for a
// non-empty cell (0 means "clear the day" on submit).
func cellMin(secs int) int {
	if secs <= 0 {
		return 0
	}
	return max(1, secs/60)
}
