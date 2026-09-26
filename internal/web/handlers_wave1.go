package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// ---------------------------------------------------------------------------
// Timesheet — week grid (activity × Mon..Sun)
// ---------------------------------------------------------------------------

type timesheetDay struct {
	Index int    // 0..6
	Label string // "Mon"
	Date  string // "22"
	ISO   string // 2026-09-22
	Secs  int
	Min   int    // minutes for the cell input
	Total string // formatted
	IsToday bool
}

type timesheetRow struct {
	ActivityID   int64
	ActivityName string
	Color        string
	Secs         [7]int
	Cells        [7]timesheetDay // copy of day headers + this row's secs
	RowTotal     int
	RowTotalLabel string
}

type timesheetData struct {
	pageData
	WeekStart     time.Time
	WeekEnd       time.Time
	PrevWeek      string // link query
	NextWeek      string
	WeekLabel     string // "Sep 22 – Sep 28"
	Days          []timesheetDay
	Rows          []timesheetRow
	DayTotals     [7]int
	DayTotalLabels [7]string
	GrandTotal    int
	GrandTotalLabel string
}

// handleTimesheet renders the weekly grid. ?date= any day inside the
// week selects it; default is today's week.
func (s *Server) handleTimesheet(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	day := now
	if v := r.URL.Query().Get("date"); v != "" {
		if t, err := timeparse.ParseDateTime(v, now); err == nil {
			day = t
		}
	}
	weekStart := startOfWeek(day)
	weekEnd := weekStart.AddDate(0, 0, 6)
	lang := string(resolveLang(r))

	grid, err := s.db.ListTimesheet(r.Context(), teamID(r), weekStart, now)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	days := make([]timesheetDay, 7)
	for i := 0; i < 7; i++ {
		d := weekStart.AddDate(0, 0, i)
		days[i] = timesheetDay{
			Index:   i,
			Label:   d.Format("Mon"),
			Date:    d.Format("2"),
			ISO:     d.Format("2006-01-02"),
			Secs:    grid.DayTotals[i],
			Min:     grid.DayTotals[i] / 60,
			Total:   fmtDuration(grid.DayTotals[i]),
			IsToday: sameDay(d, now),
		}
	}

	rows := make([]timesheetRow, 0, len(grid.Rows))
	for _, rc := range grid.Rows {
		row := timesheetRow{
			ActivityID:    rc.ActivityID,
			ActivityName:  rc.ActivityName,
			Color:         colorFor(rc.ActivityName),
			Secs:          rc.Secs,
			RowTotal:      rc.RowTotal,
			RowTotalLabel: fmtDuration(rc.RowTotal),
		}
		for i := 0; i < 7; i++ {
			row.Cells[i] = timesheetDay{
				Index: i,
				ISO:   days[i].ISO,
				Secs:  rc.Secs[i],
				Total: fmtDuration(rc.Secs[i]),
			}
		}
		rows = append(rows, row)
	}
	var dayTotalLabels [7]string
	for i := 0; i < 7; i++ {
		dayTotalLabels[i] = fmtDuration(grid.DayTotals[i])
	}

	data := timesheetData{
		pageData: pageData{
			Title: "Timesheet", Active: "timesheet", Lang: lang,
		},
		WeekStart:       weekStart,
		WeekEnd:         weekEnd,
		WeekLabel:       weekStart.Format("Jan 2") + " – " + weekEnd.Format("Jan 2"),
		PrevWeek:        weekStart.AddDate(0, 0, -7).Format("2006-01-02"),
		NextWeek:        weekStart.AddDate(0, 0, 7).Format("2006-01-02"),
		Days:            days,
		Rows:            rows,
		DayTotals:       grid.DayTotals,
		DayTotalLabels:  dayTotalLabels,
		GrandTotal:      grid.GrandTotal,
		GrandTotalLabel: fmtDuration(grid.GrandTotal),
	}
	s.renderPageForRequest(w, r, "Timesheet", "timesheet", "timesheet", &data)
}

// handleTimesheetCell writes one grid cell. Form:
//
//	activity_id, date (YYYY-MM-DD), minutes
//
// Responds with the re-rendered row so HTMX can swap it.
func (s *Server) handleTimesheetCell(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	actID, err := strconv.ParseInt(r.FormValue("activity_id"), 10, 64)
	if err != nil || actID <= 0 {
		http.Error(w, "activity_id required", 400)
		return
	}
	dateStr := strings.TrimSpace(r.FormValue("date"))
	day, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
	if err != nil {
		http.Error(w, "bad date", 400)
		return
	}
	mins, err := strconv.Atoi(strings.TrimSpace(r.FormValue("minutes")))
	if err != nil || mins < 0 || mins > 24*60 {
		http.Error(w, "minutes must be 0..1440", 400)
		return
	}
	if err := s.db.UpsertDayTotal(r.Context(), teamID(r), actID, day, mins*60); err != nil {
		s.toast(w, err.Error(), "error")
		w.WriteHeader(200)
		return
	}
	s.toastL(w, r, "toast.saved", "", "success")
	// Re-render just the row.
	s.respondTimesheetRow(w, r, actID, day)
}

func (s *Server) respondTimesheetRow(w http.ResponseWriter, r *http.Request, actID int64, day time.Time) {
	weekStart := startOfWeek(day)
	now := time.Now()
	grid, err := s.db.ListTimesheet(r.Context(), teamID(r), weekStart, now)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	lang := string(resolveLang(r))
	for _, rc := range grid.Rows {
		if rc.ActivityID != actID {
			continue
		}
		row := timesheetRow{
			ActivityID: rc.ActivityID, ActivityName: rc.ActivityName,
			Color: colorFor(rc.ActivityName), Secs: rc.Secs,
			RowTotal: rc.RowTotal, RowTotalLabel: fmtDuration(rc.RowTotal),
		}
		for i := 0; i < 7; i++ {
			d := weekStart.AddDate(0, 0, i)
			row.Cells[i] = timesheetDay{
				Index: i, ISO: d.Format("2006-01-02"),
				Secs: rc.Secs[i], Total: fmtDuration(rc.Secs[i]),
			}
		}
		_ = lang
		s.renderFragment(w, "timesheet-row", row)
		return
	}
	// Activity vanished from the grid — render an empty row shell.
	http.Error(w, "row not found", 404)
}

// startOfWeek returns Monday 00:00 of t's week (local).
func startOfWeek(t time.Time) time.Time {
	wd := (int(t.Weekday()) + 6) % 7 // Mon=0 … Sun=6
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return d.AddDate(0, 0, -wd)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// ---------------------------------------------------------------------------
// Saved reports
// ---------------------------------------------------------------------------

// handleSavedReportsCreate stores the current /stats filters as a preset.
func (s *Server) handleSavedReportsCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	name := strings.TrimSpace(r.PostForm.Get("name"))
	period := strings.TrimSpace(r.PostForm.Get("period"))
	project := strings.TrimSpace(r.PostForm.Get("project"))
	tag := strings.TrimSpace(r.PostForm.Get("tag"))
	uid := int64(0)
	if u, ok := UserFrom(r.Context()); ok {
		uid = u.ID
	}
	if name == "" {
		http.Redirect(w, r, "/stats?flash="+encodeFlash(false, "name is required"), http.StatusSeeOther)
		return
	}
	if _, err := s.db.CreateSavedReport(r.Context(), teamID(r), name, period, project, tag, uid); err != nil {
		http.Redirect(w, r, "/stats?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	// Back to the same stats view.
	next := "/stats?period=" + url.QueryEscape(period)
	if project != "" {
		next += "&project=" + url.QueryEscape(project)
	}
	if tag != "" {
		next += "&tag=" + url.QueryEscape(tag)
	}
	http.Redirect(w, r, next+"&flash="+encodeFlash(true, "report saved"), http.StatusSeeOther)
}

// handleSavedReportsDelete removes a preset.
func (s *Server) handleSavedReportsDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	if err := s.db.DeleteSavedReport(r.Context(), teamID(r), id); err != nil {
		http.Redirect(w, r, "/stats?flash="+encodeFlash(false, "not found"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/stats?flash="+encodeFlash(true, "report deleted"), http.StatusSeeOther)
}

// loadSavedReports is a helper used by handleStats.
func (s *Server) loadSavedReports(r *http.Request) []db.SavedReport {
	list, _ := s.db.ListSavedReports(r.Context(), teamID(r))
	return list
}
