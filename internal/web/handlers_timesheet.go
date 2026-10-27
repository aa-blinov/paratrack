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
	Min     int    // minutes for the cell input: stopped time only
	Total   string // formatted
	IsToday bool
	// Live is the running timer's part of Secs. A cell write replaces the
	// stopped time, so the input shows that and the live part sits beside it;
	// typing 30 used to show 33 because the running minutes were added in.
	Live int
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

func (timesheetRow) isTemplateData() {}

type timesheetData struct {
	pageData
	TimesheetReact bool
	// EditRequests are the time edit requests the viewer may see; a manager decides them (TIME-19).
	EditRequests    []timeEditView
	CanDecide       bool
	WeekStart       time.Time
	WeekEnd         time.Time
	PrevWeek        string // link query
	NextWeek        string
	WeekLabel       string // "Sep 22 to Sep 28"
	Days            []timesheetDay
	Rows            []timesheetRow
	ProjectNames    map[int64]string
	ProjectSlugs    map[int64]string
	DayTotals       [7]int
	DayTotalLabels  [7]string
	GrandTotal      int
	GrandTotalLabel string
	Others          []activityView // big workspace: activities not on the sheet
	Added           []int64        // rows added by hand this visit
	DateISO         string
	Approval        timesheetApproval
	// AutoLockNotice names the first open day when the workspace closes past
	// days for edits; empty when it does not.
	AutoLockNotice string
	// Flash is the answer of the last approve or reopen, shown above the week.
	Flash   string
	FlashOK bool
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
	projectSlugs := make(map[int64]string, len(projects))
	for _, project := range projects {
		projectNames[project.ID] = project.Name
		projectSlugs[project.ID] = project.Slug
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

	running := s.liveByDay(r, weekStart, now)
	rows := make([]timesheetRow, 0, len(grid.Rows))
	for _, rc := range grid.Rows {
		row := timesheetRow{
			ActivityID:    rc.ActivityID,
			ActivityName:  rc.ActivityName,
			ProjectID:     rc.ProjectID,
			Color:         activityColor(s.activityColors(r), rc.ActivityName),
			Secs:          rc.Secs,
			RowTotal:      rc.RowTotal,
			RowTotalLabel: fmtDur(r, rc.RowTotal),
		}
		for i := 0; i < 7; i++ {
			live := min(running[rc.ActivityID][i], rc.Secs[i])
			row.Cells[i] = timesheetDay{
				Index:   i,
				ISO:     days[i].ISO,
				Secs:    rc.Secs[i],
				Min:     cellMin(rc.Secs[i] - live),
				Total:   fmtDur(r, rc.Secs[i]),
				IsToday: days[i].IsToday,
				Live:    live,
			}
		}
		rows = append(rows, row)
	}
	var dayTotalLabels [7]string
	for i := 0; i < 7; i++ {
		dayTotalLabels[i] = fmtDur(r, grid.DayTotals[i])
	}

	approval, err := s.timesheetApprovalFor(r, weekStart, now)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	autoLockNotice, err := s.timesheetAutoLockNotice(r)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	edits, err := s.timeEditViews(r)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data := timesheetData{
		pageData: pageData{
			Title: "Timesheet", Active: "timesheet", Lang: lang, ReactApp: true,
		},
		TimesheetReact:  true,
		EditRequests:    edits,
		AutoLockNotice:  autoLockNotice,
		CanDecide:       canManage(r),
		WeekStart:       weekStart,
		WeekEnd:         weekEnd,
		WeekLabel:       periodSpan(resolveLang(r), fmtDay(resolveLang(r), weekStart), fmtDay(resolveLang(r), weekEnd)),
		PrevWeek:        weekStart.AddDate(0, 0, -7).Format("2006-01-02"),
		NextWeek:        weekStart.AddDate(0, 0, 7).Format("2006-01-02"),
		Days:            days,
		Rows:            rows,
		ProjectNames:    projectNames,
		ProjectSlugs:    projectSlugs,
		DayTotals:       grid.DayTotals,
		DayTotalLabels:  dayTotalLabels,
		GrandTotal:      grid.GrandTotal,
		GrandTotalLabel: fmtDur(r, grid.GrandTotal),
		Others:          activityViews(grid.Others, string(resolveLang(r))),
		Added:           added,
		DateISO:         weekStart.Format("2006-01-02"),
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	data.Approval = approval
	s.renderPageForRequest(w, r, "Timesheet", "timesheet", &data)
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
	// No toast: the sheet says «Сохранено: …» in its own status line, and a
	// second notice per cell was noise while a week was typed in.
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
	// No toast: the sheet says «Сохранено: …» in its own status line, and a
	// second notice per cell was noise while a week was typed in.
	s.respondTimesheetRow(w, r, actID, weekStart)
}

// respondTimesheetWriteError maps a failed grid write onto a JSON status. Billed
// time is reported as a refusal with the invoice number, not as a generic failure.
func (s *Server) respondTimesheetWriteError(w http.ResponseWriter, r *http.Request, actID int64, day time.Time, err error) {
	var approvalErr *model.SessionApprovalLockError
	if errors.As(err, &approvalErr) {
		s.respondTimesheetRow(w, r, actID, day, weekLockedMessage(resolveLang(r), userLoc(r), approvalErr))
		return
	}
	var autoLockErr *model.SessionAutoLockError
	if errors.As(err, &autoLockErr) {
		s.respondTimesheetRow(w, r, actID, day, autoLockMessage(resolveLang(r), autoLockErr))
		return
	}
	var lockErr *model.SessionInvoiceLockError
	if errors.As(err, &lockErr) {
		// Put the row back to what the invoice billed; the page shows why.
		message := fmt.Sprintf(i18n.T(resolveLang(r), "inv.locked"), lockErr.InvoiceNumber)
		s.respondTimesheetRow(w, r, actID, day, message)
		return
	}
	if errors.Is(err, appmodel.ErrTaskArchived) {
		s.respondTimesheetRow(w, r, actID, day, i18n.T(resolveLang(r), "tasks.archivedTracking"))
		return
	}
	switch {
	case errors.Is(err, appmodel.ErrInvalidSessionEdit):
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.invalidInput")})
	case errors.Is(err, model.ErrNotFound):
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "activity not found"})
	case errors.Is(err, model.ErrForbidden):
		s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "workspace membership required"})
	default:
		s.writeInternalJSONError(w, err)
	}
}

type timesheetCellJSON struct {
	ISO   string `json:"iso"`
	Secs  int    `json:"secs"`
	Min   int    `json:"min"`
	Total string `json:"total"`
	Live  int    `json:"live"`
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
		response := timesheetRowJSON{
			ActivityID: rc.ActivityID, ActivityName: rc.ActivityName, Color: activityColor(s.activityColors(r), rc.ActivityName),
			Cells: make([]timesheetCellJSON, 0, 7), RowTotal: rc.RowTotal, RowTotalLabel: fmtDur(r, rc.RowTotal),
			DayTotals: make([]timesheetDayTotalJSON, 0, 7), GrandTotal: grid.GrandTotal, GrandTotalLabel: fmtDur(r, grid.GrandTotal),
		}
		running := s.liveByDay(r, weekStart, now)
		for i := 0; i < 7; i++ {
			d := weekStart.AddDate(0, 0, i)
			live := min(running[rc.ActivityID][i], rc.Secs[i])
			response.Cells = append(response.Cells, timesheetCellJSON{ISO: d.Format("2006-01-02"), Secs: rc.Secs[i], Min: cellMin(rc.Secs[i] - live), Total: fmtDur(r, rc.Secs[i]), Live: live})
			response.DayTotals = append(response.DayTotals, timesheetDayTotalJSON{Secs: grid.DayTotals[i], Total: fmtDur(r, grid.DayTotals[i])})
		}
		if len(failure) > 0 {
			response.Error = failure[0]
		}
		s.writeJSON(w, response)
		return
	}
	// The activity is no longer in the grid.
	http.Error(w, "row not found", 404)
}

// liveByDay is each running timer's time per day of the week, by activity,
// in the same scope as the grid.
func (s *Server) liveByDay(r *http.Request, weekStart, now time.Time) map[int64][7]int {
	out := map[int64][7]int{}
	for _, item := range s.runningSessions(r) {
		days := out[item.Session.ActivityID]
		for i := range days {
			from := weekStart.AddDate(0, 0, i)
			days[i] += item.Session.TrackedSecondsInWindow(from, from.AddDate(0, 0, 1), now)
		}
		out[item.Session.ActivityID] = days
	}
	return out
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

// timesheetApproval says whether the shown week is approved, and what the viewer
// can do about it. Dates are the week's days in the viewer's location.
type timesheetApproval struct {
	Approved   bool
	Finished   bool
	ApprovedOn string
	CanManage  bool
	StartISO   string
	EndISO     string
}

// timesheetAutoLockNotice returns the sentence that tells the viewer which past
// days are closed, or an empty string when the workspace closes none.
func (s *Server) timesheetAutoLockNotice(r *http.Request) (string, error) {
	cutoff, on, err := s.services.Tracking.Queries.TimesheetAutoLockCutoff(r.Context(), teamID(r))
	if err != nil || !on {
		return "", err
	}
	lang := resolveLang(r)
	return fmt.Sprintf(i18n.T(lang, "ts.autoLockNotice"), fmtDay(lang, cutoff)), nil
}

// timesheetApprovalFor reads whether the week that starts at weekStart is approved.
func (s *Server) timesheetApprovalFor(r *http.Request, weekStart, now time.Time) (timesheetApproval, error) {
	end := weekStart.AddDate(0, 0, 7)
	view := timesheetApproval{
		Finished:  !end.After(now),
		CanManage: canManage(r),
		StartISO:  weekStart.Format("2006-01-02"),
		EndISO:    end.AddDate(0, 0, -1).Format("2006-01-02"),
	}
	items, err := s.services.Tracking.Queries.TimesheetApprovals(r.Context(), teamID(r), weekStart, end)
	if err != nil {
		return view, err
	}
	for _, item := range items {
		if !weekStart.Before(item.Start) && weekStart.Before(item.End) {
			view.Approved = true
			view.ApprovedOn = item.ApprovedAt.In(userLoc(r)).Format("02.01.2006")
			break
		}
	}
	return view, nil
}

// handleTimesheetApprove closes the week shown on the timesheet. The form sends
// its first and last day in the viewer's location.
func (s *Server) handleTimesheetApprove(w http.ResponseWriter, r *http.Request) {
	s.changeWeekApproval(w, r, true)
}

// handleTimesheetReopen removes an approval, unless a pay run already covers the week.
func (s *Server) handleTimesheetReopen(w http.ResponseWriter, r *http.Request) {
	s.changeWeekApproval(w, r, false)
}

func (s *Server) changeWeekApproval(w http.ResponseWriter, r *http.Request, approve bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	lang := resolveLang(r)
	loc := userLoc(r)
	first, err1 := time.ParseInLocation("2006-01-02", r.PostForm.Get("start"), loc)
	last, err2 := time.ParseInLocation("2006-01-02", r.PostForm.Get("end"), loc)
	if err1 != nil || err2 != nil || last.Before(first) {
		http.Redirect(w, r, "/timesheet?flash="+encodeFlash(false, i18n.T(lang, "err.invalidInput")), http.StatusSeeOther)
		return
	}
	back := "/timesheet?date=" + first.Format("2006-01-02")
	request := appmodel.TimesheetApprovalRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), Start: first, End: last.AddDate(0, 0, 1)}
	action, okKey := "timesheet.reopen", "ts.reopenDone"
	change := s.services.Tracking.Commands.ReopenWeek
	if approve {
		action, okKey = "timesheet.approve", "ts.approveDone"
		change = s.services.Tracking.Commands.ApproveWeek
	}
	if err := change(r.Context(), request); err != nil {
		http.Redirect(w, r, back+"&flash="+encodeFlash(false, approvalMessage(lang, err)), http.StatusSeeOther)
		return
	}
	s.audit(r, action, first.Format("2006-01-02")+".."+last.Format("2006-01-02"), "")
	http.Redirect(w, r, back+"&flash="+encodeFlash(true, i18n.T(lang, okKey)), http.StatusSeeOther)
}

// weekLockedMessage names the approved week and says who can reopen it. The
// store keeps the end of the span exclusive, so the last day shown is the one
// before it; the dates follow the person's timezone, not the server's.
func weekLockedMessage(lang i18n.Lang, loc *time.Location, lock *model.SessionApprovalLockError) string {
	first := lock.From.In(loc)
	last := lock.To.In(loc).AddDate(0, 0, -1)
	return fmt.Sprintf(i18n.T(lang, "ts.weekLocked"), fmtDay(lang, first), fmtDay(lang, last))
}

// autoLockMessage names the first open day. The cutoff keeps the workspace's
// zone (it is computed there), so the day is not shifted into the person's zone.
func autoLockMessage(lang i18n.Lang, lock *model.SessionAutoLockError) string {
	return fmt.Sprintf(i18n.T(lang, "ts.autoLocked"), fmtDay(lang, lock.Before))
}

// approvalMessage turns an approval refusal into the sentence the manager reads.
func approvalMessage(lang i18n.Lang, err error) string {
	switch {
	case errors.Is(err, model.ErrWeekNotFinished):
		return i18n.T(lang, "ts.notFinished")
	case errors.Is(err, model.ErrWeekHasRunningTimer):
		return i18n.T(lang, "ts.approveRunning")
	case errors.Is(err, model.ErrWeekPaid):
		return i18n.T(lang, "ts.reopenPaid")
	default:
		return i18n.T(lang, "err.generic")
	}
}
