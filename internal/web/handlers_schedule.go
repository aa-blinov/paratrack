package web

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// schedDay is one day of the week: the header, and each cell's own day.
type schedDay struct {
	Index   int
	Label   string
	Date    string
	ISO     string
	Min     int
	Total   string
	IsToday bool
}

// schedLine is one project in one person's week. The cells are that
// project's minutes, editable by a manager.
type schedLine struct {
	ProjectID   int64
	ProjectName string
	Color       string
	// CanEdit: the viewer may write this project's hours: a manager, or the
	// lead of this project.
	CanEdit  bool
	Cells    [7]schedDay
	TotalMin int
	Total    string
}

// schedPerson is one member's block: the header carries the week's numbers,
// the lines carry the projects they are planned on.
type schedPerson struct {
	UserID   int64
	UserName string
	// AdminMin is the admin time kept each week; it comes off AvailableMin.
	AdminMin int
	// AvailableMin is what the week can carry for clients: capacity for five
	// days less admin. LoadPct and the red overload are measured against it.
	AvailableMin   int
	AvailableLabel string
	PlannedMin     int
	Total          string
	LoadPct        int
	// Tracked is what the person logged in the week. Only managers get it:
	// members see the plan, not their colleagues' time.
	TrackedLabel string
	TrackedPct   int
	Lines        []schedLine
}

type schedulePage struct {
	pageData
	ScheduleReact bool
	WeekLabel     string
	PrevWeek      string
	NextWeek      string
	ThisWeek      string
	Days          []schedDay
	People        []schedPerson
	Projects      []projectView
	GrandTotal    string
	GrandMin      int
	CanManage     bool
	// CopyFrom: the previous week has a plan to repeat. WeekEmpty: this week
	// has none, so it can take a copy.
	CopyFrom  bool
	WeekEmpty bool
	// DurationFormat is the user's time format ("hm", "decimal" or "clock"):
	// the page shows its live totals in the same format the server uses.
	DurationFormat string
	// EditableProjects are the projects this viewer may add a line for.
	EditableProjects []projectView
}

func (p *schedulePage) setCSRF(t string)   { p.pageData.setCSRF(t) }
func (p *schedulePage) usesReactApp() bool { return p.ScheduleReact }

// schedPlan is one week as the page and the plan endpoints send it.
type schedPlan struct {
	Days       []schedDay
	People     []schedPerson
	GrandTotal string
	GrandMin   int
	WeekEmpty  bool
}

// schedulePlanResponse is the JSON answer to a plan change: the week again,
// so the page replaces its state without a reload.
type schedulePlanResponse struct {
	Days       []schedDay    `json:"days"`
	People     []schedPerson `json:"people"`
	GrandTotal string        `json:"grandTotal"`
	GrandMin   int           `json:"grandMin"`
	WeekEmpty  bool          `json:"weekEmpty"`
	CopyFrom   bool          `json:"copyFrom"`
}

func (schedulePlanResponse) isJSONResponse() {}

// handleSchedule renders the week: a block per person, a line per project.
func (s *Server) handleSchedule(w http.ResponseWriter, r *http.Request) {
	now := userNow(r)
	day := now
	if v := r.URL.Query().Get("date"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			day = t
		}
	}
	weekStart := startOfWeek(r, day)
	resolvedLang := resolveLang(r)
	lang := string(resolvedLang)
	plan, projects, err := s.loadSchedulePlan(r, weekStart)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	copyFrom, err := s.hasSchedulePlan(r, weekStart.AddDate(0, 0, -7))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data := schedulePage{
		pageData:         pageData{Title: "Schedule", Active: "schedule", Lang: lang, ReactApp: true},
		ScheduleReact:    true,
		WeekLabel:        periodSpan(resolvedLang, fmtDay(resolvedLang, weekStart), fmtDay(resolvedLang, weekStart.AddDate(0, 0, 6))),
		PrevWeek:         weekStart.AddDate(0, 0, -7).Format("2006-01-02"),
		NextWeek:         weekStart.AddDate(0, 0, 7).Format("2006-01-02"),
		ThisWeek:         weekStart.Format("2006-01-02"),
		Days:             plan.Days,
		People:           plan.People,
		Projects:         projects,
		GrandTotal:       plan.GrandTotal,
		GrandMin:         plan.GrandMin,
		CanManage:        canManage(r),
		CopyFrom:         copyFrom,
		WeekEmpty:        plan.WeekEmpty,
		DurationFormat:   durFmtOf(r),
		EditableProjects: editableProjects(projects, canManage(r), authenticatedUserID(r)),
	}
	s.renderPageForRequest(w, r, "Schedule", "schedule", &data)
}

// loadSchedulePlan builds one week for the page: people in name order, each
// with the projects they have minutes on this week, by project name.
func (s *Server) loadSchedulePlan(r *http.Request, weekStart time.Time) (schedPlan, []projectView, error) {
	now := userNow(r)
	snapshot, err := s.services.Scheduling.List(r.Context(), appmodel.ScheduleQuery{TeamID: teamID(r), WeekStart: weekStart, Now: now})
	if err != nil {
		return schedPlan{}, nil, err
	}
	days := make([]schedDay, 7)
	for i := range days {
		d := weekStart.AddDate(0, 0, i)
		days[i] = schedDay{
			Index: i, Label: fmtWeekday(resolveLang(r), d), Date: d.Format("2"),
			ISO: d.Format("2006-01-02"), IsToday: sameDay(d, now),
		}
	}
	colors := make(map[int64]string, len(snapshot.Projects))
	leads := make(map[int64]int64, len(snapshot.Projects))
	for _, project := range snapshot.Projects {
		colors[project.ID] = project.Color
		leads[project.ID] = project.LeadUserID
	}
	viewer := authenticatedUserID(r)
	canEditLine := func(pid int64) bool {
		return canManage(r) || (viewer > 0 && leads[pid] == viewer)
	}
	showTracked := canManage(r)
	plan := schedPlan{Days: days, People: []schedPerson{}, GrandMin: snapshot.TotalMinutes, GrandTotal: fmtDur(r, snapshot.TotalMinutes*60)}
	for _, rc := range snapshot.Rows {
		person := schedPerson{
			UserID: rc.UserID, UserName: rc.UserName, AdminMin: rc.AdminMinutes, AvailableMin: rc.AvailableMinutes,
			AvailableLabel: fmtDur(r, rc.AvailableMinutes*60), Lines: []schedLine{},
			PlannedMin: rc.Total, Total: fmtDur(r, rc.Total*60), LoadPct: rc.LoadPercent,
		}
		if showTracked {
			person.TrackedLabel = fmtDur(r, rc.Tracked*60)
			person.TrackedPct = rc.TrackedPercent
		}
		var projectIDs []int64
		// Every project in ByProject is on this person's schedule: one with
		// hours, or a line that has no hours yet.
		for pid := range rc.ByProject {
			projectIDs = append(projectIDs, pid)
		}
		sort.Slice(projectIDs, func(i, j int) bool {
			a, b := snapshot.ProjectNames[projectIDs[i]], snapshot.ProjectNames[projectIDs[j]]
			if a != b {
				return a < b
			}
			return projectIDs[i] < projectIDs[j]
		})
		for _, pid := range projectIDs {
			cells := rc.ByProject[pid]
			line := schedLine{ProjectID: pid, ProjectName: snapshot.ProjectNames[pid], Color: colors[pid], CanEdit: canEditLine(pid)}
			for i := 0; i < 7; i++ {
				line.Cells[i] = schedDay{Index: i, ISO: days[i].ISO, Min: cells[i], Total: fmtDur(r, cells[i]*60), IsToday: days[i].IsToday}
				line.TotalMin += cells[i]
			}
			line.Total = fmtDur(r, line.TotalMin*60)
			person.Lines = append(person.Lines, line)
		}
		for i := range days {
			days[i].Min += rc.Minutes[i]
		}
		plan.People = append(plan.People, person)
	}
	for i := range plan.Days {
		plan.Days[i].Total = fmtDur(r, plan.Days[i].Min*60)
	}
	plan.WeekEmpty = snapshot.TotalMinutes == 0
	return plan, projectViews(snapshot.Projects), nil
}

// hasSchedulePlan reports whether any member has planned time that week.
func (s *Server) hasSchedulePlan(r *http.Request, weekStart time.Time) (bool, error) {
	snapshot, err := s.services.Scheduling.List(r.Context(), appmodel.ScheduleQuery{TeamID: teamID(r), WeekStart: weekStart, Now: userNow(r)})
	if err != nil {
		return false, err
	}
	return snapshot.TotalMinutes > 0, nil
}

// handleScheduleCell writes one plan cell.
// Form: user_id, project_id, date (YYYY-MM-DD), minutes.
func (s *Server) handleScheduleCell(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	uid, err1 := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
	projectValue := strings.TrimSpace(r.PostForm.Get("project_id"))
	var pid int64
	if projectValue != "" {
		var err error
		pid, err = strconv.ParseInt(projectValue, 10, 64)
		if err != nil || pid < 0 {
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "bad project_id"})
			return
		}
	}
	mins, err3 := strconv.Atoi(strings.TrimSpace(r.PostForm.Get("minutes")))
	day := strings.TrimSpace(r.PostForm.Get("date"))
	if err1 != nil || uid <= 0 || err3 != nil || day == "" {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "user_id, date, minutes required"})
		return
	}
	parsedDay, err := time.Parse("2006-01-02", day)
	if err != nil || parsedDay.Format("2006-01-02") != day {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "date must be YYYY-MM-DD"})
		return
	}
	if err := s.services.Scheduling.SetCell(r.Context(), appmodel.ScheduleCellRequest{
		TeamID: teamID(r), ActorID: authenticatedUserID(r), UserID: uid, ProjectID: pid,
		Day: parsedDay, Minutes: mins,
	}); err != nil {
		switch {
		case errors.Is(err, appmodel.ErrNoScheduleProjects):
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.needProject")})
		case errors.Is(err, appmodel.ErrInvalidScheduleCell):
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.invalidInput")})
		case errors.Is(err, model.ErrForbidden):
			s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "forbidden"})
		default:
			s.writeInternalJSONError(w, err)
		}
		return
	}
	s.toastL(w, r, "toast.saved", "", "success")
	s.respondSchedulePlan(w, r, startOfWeek(r, parsedDay))
}

// handleScheduleCopy repeats the previous week in the week given by `week`.
// The rule is checked on the server too: a week that already has a plan is
// refused, never merged, and the page says why.
func (s *Server) handleScheduleCopy(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(r.PostForm.Get("week")))
	if err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "week must be YYYY-MM-DD"})
		return
	}
	target := startOfWeek(r, parsed)
	if err := s.services.Scheduling.CopyScheduleWeek(r.Context(), appmodel.ScheduleCopyRequest{
		TeamID: teamID(r), ActorID: authenticatedUserID(r), From: target.AddDate(0, 0, -7), To: target,
	}); err != nil {
		switch {
		case errors.Is(err, appmodel.ErrScheduleWeekNotEmpty):
			s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: i18n.T(resolveLang(r), "sch.copyNeedEmpty")})
		case errors.Is(err, appmodel.ErrScheduleNothingToCopy):
			s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: i18n.T(resolveLang(r), "sch.copyNoPrev")})
		case errors.Is(err, appmodel.ErrInvalidScheduleCell):
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.invalidInput")})
		default:
			s.writeInternalJSONError(w, err)
		}
		return
	}
	s.toastL(w, r, "sch.copyDone", "", "success")
	s.respondSchedulePlan(w, r, target)
}

// handleScheduleAdmin sets a member's weekly admin time, in minutes.
// Form: user_id, minutes, week (the week on screen, to answer with).
func (s *Server) handleScheduleAdmin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return
	}
	uid, err1 := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("user_id")), 10, 64)
	minutes, err2 := strconv.Atoi(strings.TrimSpace(r.PostForm.Get("minutes")))
	week, err3 := time.Parse("2006-01-02", strings.TrimSpace(r.PostForm.Get("week")))
	if err1 != nil || err2 != nil || err3 != nil || uid <= 0 || minutes < 0 {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "user_id, minutes, week required"})
		return
	}
	if err := s.services.Scheduling.SetScheduleAdminMinutes(r.Context(), appmodel.ScheduleAdminRequest{
		TeamID: teamID(r), ActorID: authenticatedUserID(r), UserID: uid, Minutes: minutes,
	}); err != nil {
		switch {
		case errors.Is(err, appmodel.ErrInvalidScheduleCell):
			s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.invalidInput")})
		case errors.Is(err, appmodel.ErrNotFound):
			s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
		default:
			s.writeInternalJSONError(w, err)
		}
		return
	}
	s.toastL(w, r, "toast.saved", "", "success")
	s.respondSchedulePlan(w, r, startOfWeek(r, week))
}

// respondSchedulePlan answers a plan change with the week it touched.
func (s *Server) respondSchedulePlan(w http.ResponseWriter, r *http.Request, weekStart time.Time) {
	plan, _, err := s.loadSchedulePlan(r, weekStart)
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	copyFrom, err := s.hasSchedulePlan(r, weekStart.AddDate(0, 0, -7))
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	s.writeJSON(w, schedulePlanResponse{
		Days: plan.Days, People: plan.People, GrandTotal: plan.GrandTotal, GrandMin: plan.GrandMin,
		WeekEmpty: plan.WeekEmpty, CopyFrom: copyFrom,
	})
}

// editableProjects are the projects a viewer may add a plan line for: every
// project for a manager, and only the ones they lead for anyone else.
func editableProjects(projects []projectView, manager bool, viewer int64) []projectView {
	if manager {
		return projects
	}
	out := []projectView{} // never null: the page reads it as a list
	for _, project := range projects {
		if project.LeadUserID == viewer && viewer > 0 {
			out = append(out, project)
		}
	}
	return out
}

// handleScheduleLine puts a project on a person's schedule, with no hours yet.
// Form: user_id, project_id, week (the week on screen, to answer with).
func (s *Server) handleScheduleLine(w http.ResponseWriter, r *http.Request) {
	request, week, ok := s.scheduleLineForm(w, r)
	if !ok {
		return
	}
	if err := s.services.Scheduling.AddLine(r.Context(), request); err != nil {
		s.writeScheduleLineError(w, r, err)
		return
	}
	s.toastL(w, r, "toast.saved", "", "success")
	s.respondSchedulePlan(w, r, week)
}

// handleScheduleLineRemove takes a project off a person's schedule. A line that
// still has hours is refused: the hours would go with it.
func (s *Server) handleScheduleLineRemove(w http.ResponseWriter, r *http.Request) {
	request, week, ok := s.scheduleLineForm(w, r)
	if !ok {
		return
	}
	if err := s.services.Scheduling.RemoveLine(r.Context(), request); err != nil {
		s.writeScheduleLineError(w, r, err)
		return
	}
	s.toastL(w, r, "toast.saved", "", "success")
	s.respondSchedulePlan(w, r, week)
}

func (s *Server) scheduleLineForm(w http.ResponseWriter, r *http.Request) (appmodel.ScheduleLineRequest, time.Time, bool) {
	if err := r.ParseForm(); err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "invalid form"})
		return appmodel.ScheduleLineRequest{}, time.Time{}, false
	}
	uid, err1 := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("user_id")), 10, 64)
	pid, err2 := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("project_id")), 10, 64)
	day, err3 := time.Parse("2006-01-02", strings.TrimSpace(r.PostForm.Get("week")))
	if err1 != nil || err2 != nil || err3 != nil || uid <= 0 || pid <= 0 {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: "user_id, project_id, week required"})
		return appmodel.ScheduleLineRequest{}, time.Time{}, false
	}
	request := appmodel.ScheduleLineRequest{TeamID: teamID(r), ActorID: authenticatedUserID(r), UserID: uid, ProjectID: pid}
	return request, startOfWeek(r, day), true
}

func (s *Server) writeScheduleLineError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, appmodel.ErrScheduleLineHasHours):
		s.writeJSONStatus(w, http.StatusConflict, apiErrorResponse{Error: i18n.T(resolveLang(r), "sch.lineHasHours")})
	case errors.Is(err, model.ErrForbidden):
		s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: "forbidden"})
	case errors.Is(err, model.ErrNotFound), errors.Is(err, appmodel.ErrNotFound):
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: "not found"})
	case errors.Is(err, appmodel.ErrInvalidScheduleCell):
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.invalidInput")})
	default:
		s.writeInternalJSONError(w, err)
	}
}
