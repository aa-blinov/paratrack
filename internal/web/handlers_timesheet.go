package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
	"github.com/aa-blinov/paratrack/internal/requestctx"
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

type timesheetTeamMemberStatus struct {
	UserID       int64
	Name         string
	TrackedLabel string
	NormLabel    string
	Percent      int
	MissingDays  int
	Approved     bool
	Status       string // "approved" | "ok" | "away" | "warn" | "empty"
	// AwayLabel names the member's absences in the week ("отпуск: 3 дн.").
	AwayLabel string
}

// absenceSpanVM is a run of consecutive days one member is away for one reason.
type absenceSpanVM struct {
	UserID int64
	Name   string
	Kind   string
	Label  string
	From   string
	To     string
}

// absenceSpans folds the days into runs per member and reason, for the list
// the manager reads and removes from.
func absenceSpans(lang i18n.Lang, loc *time.Location, absences []appmodel.Absence, names map[int64]string) []absenceSpanVM {
	var out []absenceSpanVM
	for _, a := range absences {
		day, err := time.ParseInLocation("2006-01-02", a.Day, loc)
		if err != nil {
			continue
		}
		merged := false
		for i := range out {
			last, _ := time.ParseInLocation("2006-01-02", out[i].To, loc)
			if out[i].UserID == a.UserID && out[i].Kind == a.Kind && last.AddDate(0, 0, 1).Equal(day) {
				out[i].To = a.Day
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, absenceSpanVM{UserID: a.UserID, Name: names[a.UserID], Kind: a.Kind, From: a.Day, To: a.Day})
		}
	}
	for i := range out {
		from, _ := time.ParseInLocation("2006-01-02", out[i].From, loc)
		to, _ := time.ParseInLocation("2006-01-02", out[i].To, loc)
		span := fmtDay(lang, from)
		if out[i].To != out[i].From {
			span = periodSpan(lang, fmtDay(lang, from), fmtDay(lang, to))
		}
		out[i].Label = dayMarkLabel(lang, out[i].Kind) + ", " + span
	}
	return out
}

// sortedKeys is a map's keys in order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

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
	// PlannedByProject carries this week's scheduled minutes per project from
	// the schedule, so a row can say how its tracked time stands against the
	// plan. Projects without a plan are absent from the map.
	PlannedByProject map[int64]int
	// Person is the member whose sheet is on screen (0 = the whole workspace
	// for a manager, or the viewer's own for a member). People fills the
	// manager's selector (TIME-24).
	Person      int64
	People      []personOption
	TeamSummary []timesheetTeamMemberStatus
	Approval    timesheetApproval
	// AutoLockNotice names the first open day when the workspace closes past
	// days for edits; empty when it does not.
	AutoLockNotice string
	// Flash is the answer of the last approve or reopen, shown above the week.
	Flash   string
	FlashOK bool
	// Holidays names the workspace's days off in the week, for a manager.
	Holidays []string
	// Absences lists, for a manager, who is away from a week before this one
	// to two months after, as spans that can be removed.
	Absences []absenceSpanVM
}

// weekPlanByProject sums the schedule's per-project plan for the week the
// sheet shows, across every member. The schedule stores per-day cells; the
// row only needs the week's total to say "plan 16 h, tracked 14 h".
func (s *Server) weekPlanByProject(r *http.Request, weekStart, now time.Time) (map[int64]int, error) {
	snapshot, err := s.services.Scheduling.List(r.Context(), appmodel.ScheduleQuery{TeamID: teamID(r), WeekStart: weekStart, Now: now})
	if err != nil {
		return nil, fmt.Errorf("load the week's plan: %w", err)
	}
	planned := map[int64]int{}
	for _, row := range snapshot.Rows {
		for projectID, minutes := range row.ByProject {
			for _, day := range minutes {
				planned[projectID] += day
			}
		}
	}
	return planned, nil
}

// handleTimesheet renders the weekly grid. ?date= any day inside the
// week selects it; default is today's week. For a manager, ?person=<id>
// shows one member's sheet instead of the whole workspace's (TIME-24):
// the read is scoped to that member's sessions the same way a member's own
// view is.
func (s *Server) handleTimesheet(w http.ResponseWriter, r *http.Request) {
	now := userNow(r)
	day := now
	if v := r.URL.Query().Get("date"); v != "" {
		if t, err := timeparse.ParseDateTime(v, now); err == nil {
			day = t
		}
	}
	personID := int64(0)
	if canManage(r) {
		if v := r.URL.Query().Get("person"); v != "" {
			if id, err := strconv.ParseInt(v, 10, 64); err == nil && id > 0 {
				for _, candidate := range s.taskPeople(r) {
					if candidate.ID == id {
						personID = id
						break
					}
				}
			}
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
	// The member view is a read: the grid is scoped to that person's sessions
	// the way a member's own view is, while the caller keeps manager rights for
	// approvals and the week status above the sheet.
	gridCtx := r.Context()
	if personID > 0 {
		gridCtx = requestctx.WithScope(gridCtx, personID)
	}
	grid, err := s.services.Tracking.Queries.Timesheet(gridCtx, appmodel.TimesheetRequest{
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
	colors := s.activityColors(r)
	for _, rc := range grid.Rows {
		row := timesheetRow{
			ActivityID:    rc.ActivityID,
			ActivityName:  rc.ActivityName,
			ProjectID:     rc.ProjectID,
			Color:         activityColor(colors, rc.ActivityName),
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
	// The week's plan, by project, straight from the schedule the same week
	// reads. A row of the sheet answers "how much of the plan did this
	// project's time cover" instead of standing alone.
	plannedByProject, err := s.weekPlanByProject(r, weekStart, now)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	// The manager's selector of whose sheet is on screen. A member gets none:
	// their sheet is always their own.
	var people []personOption
	if canManage(r) {
		people = s.taskPeople(r)
	}
	data := timesheetData{
		pageData: pageData{
			Title: "Timesheet", Active: "timesheet", Lang: lang, ReactApp: true,
		},
		TimesheetReact:   true,
		EditRequests:     edits,
		AutoLockNotice:   autoLockNotice,
		CanDecide:        canManage(r),
		WeekStart:        weekStart,
		WeekEnd:          weekEnd,
		WeekLabel:        periodSpan(resolveLang(r), fmtDay(resolveLang(r), weekStart), fmtDay(resolveLang(r), weekEnd)),
		PrevWeek:         weekStart.AddDate(0, 0, -7).Format("2006-01-02"),
		NextWeek:         weekStart.AddDate(0, 0, 7).Format("2006-01-02"),
		Days:             days,
		Rows:             rows,
		ProjectNames:     projectNames,
		ProjectSlugs:     projectSlugs,
		PlannedByProject: plannedByProject,
		DayTotals:        grid.DayTotals,
		DayTotalLabels:   dayTotalLabels,
		GrandTotal:       grid.GrandTotal,
		GrandTotalLabel:  fmtDur(r, grid.GrandTotal),
		Others:           activityViews(grid.Others, string(resolveLang(r))),
		Added:            added,
		DateISO:          weekStart.Format("2006-01-02"),
		Person:           personID,
		People:           people,
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	data.Approval = approval
	data.TeamSummary = s.timesheetTeamSummary(r, weekStart, now, approval)
	if canManage(r) {
		if hol, err := s.services.Scheduling.TeamHolidays(r.Context(), teamID(r), weekStart.Format("2006-01-02"), weekStart.AddDate(0, 0, 7).Format("2006-01-02")); err == nil {
			for _, d := range sortedKeys(hol) {
				day, _ := time.ParseInLocation("2006-01-02", d, userLoc(r))
				label := fmtDay(resolveLang(r), day)
				if hol[d] != "" {
					label += " (" + hol[d] + ")"
				}
				data.Holidays = append(data.Holidays, label)
			}
		}
		names := map[int64]string{}
		for _, p := range people {
			names[p.ID] = p.Name
		}
		if list, err := s.services.Scheduling.Absences(r.Context(), teamID(r), weekStart.AddDate(0, 0, -7).Format("2006-01-02"), weekStart.AddDate(0, 0, 63).Format("2006-01-02")); err == nil {
			data.Absences = absenceSpans(resolveLang(r), userLoc(r), list, names)
		}
	}
	s.renderPageForRequest(w, r, "Timesheet", "timesheet", &data)
}

// handleTimesheetCell writes one grid cell. Form:
//
//	activity_id, date (YYYY-MM-DD), minutes
func (s *Server) handleTimesheetCell(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	actID, err := strconv.ParseInt(r.FormValue("activity_id"), 10, 64)
	if err != nil || actID <= 0 {
		http.Error(w, i18n.T(resolveLang(r), "err.activityRequired"), 400)
		return
	}
	dateStr := strings.TrimSpace(r.FormValue("date"))
	day, err := time.ParseInLocation("2006-01-02", dateStr, userLoc(r))
	if err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.badDate"), 400)
		return
	}
	mins, err := strconv.Atoi(strings.TrimSpace(r.FormValue("minutes")))
	if err != nil || mins < 0 || mins > 24*60 {
		http.Error(w, i18n.T(resolveLang(r), "err.minutesRange"), 400)
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
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	actID, err := strconv.ParseInt(r.FormValue("activity_id"), 10, 64)
	if err != nil || actID <= 0 {
		http.Error(w, i18n.T(resolveLang(r), "err.activityRequired"), 400)
		return
	}
	dateStr := strings.TrimSpace(r.FormValue("date"))
	day, err := time.ParseInLocation("2006-01-02", dateStr, userLoc(r))
	if err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.badDate"), 400)
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
	var periodErr *model.PeriodClosedError
	if errors.As(err, &periodErr) {
		s.respondTimesheetRow(w, r, actID, day, monthClosedMessage(resolveLang(r), periodErr))
		return
	}
	if errors.Is(err, model.ErrCellBelowIntervals) {
		s.respondTimesheetRow(w, r, actID, day, i18n.T(resolveLang(r), "ts.belowIntervals"))
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
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.activityNotFound")})
	case errors.Is(err, model.ErrForbidden):
		s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.forbiddenAction")})
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
	http.Error(w, i18n.T(resolveLang(r), "err.notFoundEntity"), 404)
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
	// MemberApprovals names the members whose own approval closes the week,
	// besides a possible whole-workspace one. The manager sees who is closed
	// and can reopen one person without touching the rest (TIME-23).
	MemberApprovals []timesheetMemberApproval
}

type timesheetMemberApproval struct {
	UserID     int64
	UserName   string
	ApprovedOn string
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
	names := s.taskPeople(r)
	byID := make(map[int64]string, len(names))
	for _, person := range names {
		byID[person.ID] = person.Name
	}
	for _, item := range items {
		// Only this exact week's rows count: the query window can also return
		// approvals of a neighbouring week that merely overlaps the bounds.
		if !weekStart.Equal(item.Start) {
			continue
		}
		if item.UserID == 0 {
			view.Approved = true
			view.ApprovedOn = item.ApprovedAt.In(userLoc(r)).Format("02.01.2006")
			continue
		}
		view.MemberApprovals = append(view.MemberApprovals, timesheetMemberApproval{
			UserID: item.UserID, UserName: byID[item.UserID], ApprovedOn: item.ApprovedAt.In(userLoc(r)).Format("02.01.2006"),
		})
	}
	return view, nil
}

func (s *Server) timesheetTeamSummary(r *http.Request, weekStart, now time.Time, approval timesheetApproval) []timesheetTeamMemberStatus {
	if !canManage(r) {
		return nil
	}
	snapshot, err := s.services.MemberAdmin.Management(r.Context(), teamID(r))
	if err != nil || len(snapshot.Members) <= 1 {
		return nil
	}
	capByID := make(map[int64]int, len(snapshot.PaySettings))
	workDaysByID := make(map[int64]string, len(snapshot.PaySettings))
	for _, ps := range snapshot.PaySettings {
		if ps.CapacityMinutes > 0 {
			capByID[ps.UserID] = ps.CapacityMinutes
		}
		workDaysByID[ps.UserID] = ps.WorkDays
	}
	days := make([]time.Time, 7)
	for i := range days {
		days[i] = weekStart.AddDate(0, 0, i)
	}
	tracked, err := s.services.Scheduling.TrackedSecondsByPersonDay(r.Context(), appmodel.PersonDayQuery{
		TeamID: teamID(r), Days: days, Now: now,
	})
	if err != nil {
		return nil
	}
	// Absences and the workspace's days off leave the norm: a week with a
	// holiday or a vacation is judged on the days that were to be worked.
	from, until := days[0].Format("2006-01-02"), weekStart.AddDate(0, 0, 7).Format("2006-01-02")
	holidays, err := s.services.Scheduling.TeamHolidays(r.Context(), teamID(r), from, until)
	if err != nil {
		return nil
	}
	workdays, err := s.services.Scheduling.TeamWorkdays(r.Context(), teamID(r), from, until)
	if err != nil {
		return nil
	}
	absences, err := s.services.Scheduling.Absences(r.Context(), teamID(r), from, until)
	if err != nil {
		return nil
	}
	absent := map[int64]map[string]string{}
	for _, a := range absences {
		if absent[a.UserID] == nil {
			absent[a.UserID] = map[string]string{}
		}
		absent[a.UserID][a.Day] = a.Kind
	}
	approvedByID := make(map[int64]bool, len(approval.MemberApprovals))
	for _, ma := range approval.MemberApprovals {
		approvedByID[ma.UserID] = true
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	lang := resolveLang(r)
	out := make([]timesheetTeamMemberStatus, 0, len(snapshot.Members))
	for _, m := range snapshot.Members {
		name := m.Name
		if name == "" {
			name = m.Email
		}
		perDay := tracked[m.UserID]
		totalSecs := 0
		missingDays := 0
		workDays := 0
		awayByKind := map[string]int{}
		for i, d := range days {
			sec := 0
			if i < len(perDay) {
				sec = perDay[i]
			}
			totalSecs += sec
			// A weekday by the calendar, not by its place in the week: a
			// workspace whose weeks start on Sunday has Sunday at index 0.
			key := d.Format("2006-01-02")
			_, worked := workdays[key]
			weekday := model.WorksOn(workDaysByID[m.UserID], d.Weekday()) || worked
			if kind := absent[m.UserID][key]; kind != "" && weekday {
				awayByKind[kind]++
				continue
			}
			if _, off := holidays[key]; off || !weekday {
				continue
			}
			workDays++
			if !d.After(today) && sec == 0 {
				missingDays++
			}
		}
		capMin := capByID[m.UserID]
		if capMin <= 0 {
			capMin = 480
		}
		normMin := capMin * workDays
		var awayParts []string
		for _, kind := range []string{"vacation", "sick", "dayoff"} {
			if n := awayByKind[kind]; n > 0 {
				awayParts = append(awayParts, fmt.Sprintf(i18n.T(lang, "absence.days"), dayMarkLabel(lang, kind), n))
			}
		}
		pct := 0
		if normMin > 0 {
			pct = (totalSecs / 60) * 100 / normMin
		}
		isApproved := approval.Approved || approvedByID[m.UserID]
		status := "warn"
		switch {
		case isApproved:
			status = "approved"
		case workDays == 0 && totalSecs == 0:
			// Away, or nothing to work, the whole week: on track by definition.
			status = "away"
		case totalSecs == 0:
			status = "empty"
		case (pct >= 80 || normMin == 0) && missingDays == 0:
			status = "ok"
		}
		out = append(out, timesheetTeamMemberStatus{
			UserID:       m.UserID,
			Name:         name,
			TrackedLabel: fmtDur(r, totalSecs),
			NormLabel:    fmtHoursL(lang, money.HoursHundredths(normMin*60)),
			Percent:      pct,
			MissingDays:  missingDays,
			Approved:     isApproved,
			Status:       status,
			AwayLabel:    strings.Join(awayParts, ", "),
		})
	}
	rank := map[string]int{"empty": 0, "warn": 1, "ok": 2, "away": 2, "approved": 3}
	sort.Slice(out, func(i, j int) bool {
		if rank[out[i].Status] != rank[out[j].Status] {
			return rank[out[i].Status] < rank[out[j].Status]
		}
		return out[i].Name < out[j].Name
	})
	return out
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
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
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
	// user_id picks whose week the form closes: 0 for the whole workspace, or
	// one member (TIME-23). A bad value falls back to the workspace-wide read
	// rather than failing the form.
	userID, _ := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
	if userID < 0 {
		userID = 0
	}
	back := "/timesheet?date=" + first.Format("2006-01-02")
	if person := r.URL.Query().Get("person"); person != "" {
		back += "&person=" + url.QueryEscape(person)
	}
	request := appmodel.TimesheetApprovalRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), Start: first, End: last.AddDate(0, 0, 1), UserID: userID}
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

// handleTimesheetApproveOnTrack approves, one by one, everyone the team
// summary shows on track for the week (80% of their norm and no empty
// weekday). The summary is worked out again here rather than taken from the
// form, so a week that changed since the page loaded is judged as it is now;
// the flash names who was approved and who was not.
func (s *Server) handleTimesheetApproveOnTrack(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	lang := resolveLang(r)
	loc := userLoc(r)
	day, err := time.ParseInLocation("2006-01-02", r.PostForm.Get("start"), loc)
	if err != nil {
		http.Redirect(w, r, "/timesheet?flash="+encodeFlash(false, i18n.T(lang, "err.invalidInput")), http.StatusSeeOther)
		return
	}
	// The form names a week by its first day; any other day is taken to mean
	// its week, never an arbitrary seven-day span.
	weekStart := startOfWeek(r, day)
	back := "/timesheet?date=" + weekStart.Format("2006-01-02")
	if person := r.URL.Query().Get("person"); person != "" {
		back += "&person=" + url.QueryEscape(person)
	}
	now := userNow(r)
	approval, err := s.timesheetApprovalFor(r, weekStart, now)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	if !approval.Finished {
		http.Redirect(w, r, back+"&flash="+encodeFlash(false, i18n.T(lang, "ts.notFinished")), http.StatusSeeOther)
		return
	}
	var approved, failed []string
	for _, member := range s.timesheetTeamSummary(r, weekStart, now, approval) {
		if member.Status != "ok" && member.Status != "away" {
			continue
		}
		request := appmodel.TimesheetApprovalRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), Start: weekStart, End: weekStart.AddDate(0, 0, 7), UserID: member.UserID}
		if err := s.services.Tracking.Commands.ApproveWeek(r.Context(), request); err != nil {
			failed = append(failed, member.Name+": "+approvalMessage(lang, err))
			continue
		}
		approved = append(approved, member.Name)
		s.audit(r, "timesheet.approve", weekStart.Format("2006-01-02")+".."+weekStart.AddDate(0, 0, 6).Format("2006-01-02"), member.Name)
	}
	switch {
	case len(approved) == 0 && len(failed) == 0:
		http.Redirect(w, r, back+"&flash="+encodeFlash(false, i18n.T(lang, "ts.onTrackNone")), http.StatusSeeOther)
	case len(failed) == 0:
		http.Redirect(w, r, back+"&flash="+encodeFlash(true, fmt.Sprintf(i18n.T(lang, "ts.onTrackDone"), strings.Join(approved, ", "))), http.StatusSeeOther)
	default:
		msg := fmt.Sprintf(i18n.T(lang, "ts.onTrackPartial"), strings.Join(approved, ", "), strings.Join(failed, "; "))
		http.Redirect(w, r, back+"&flash="+encodeFlash(false, msg), http.StatusSeeOther)
	}
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
