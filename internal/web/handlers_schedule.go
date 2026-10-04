package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------------------------------------------------------------------------

type schedDay struct {
	Index   int
	Label   string
	Date    string
	ISO     string
	Min     int
	Total   string
	IsToday bool
}

type schedRow struct {
	UserID   int64
	UserName string
	Capacity int
	Cells    [7]schedDay
	Total    string
	TotalMin int
	LoadPct  int // total / (capacity*7)
}

type schedulePage struct {
	pageData
	WeekLabel    string
	PrevWeek     string
	NextWeek     string
	ThisWeek     string
	ProjectID    int64 // the project whose plan the cells edit
	Days         []schedDay
	Rows         []schedRow
	RowVMs       []schedRowVM
	Projects     []projectView
	ProjectNames map[int64]string
	GrandTotal   string
	GrandMin     int
}

func (p *schedulePage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handleSchedule renders the people × week planning grid.
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
	schedule, err := s.scheduleRows(r, weekStart, r.URL.Query().Get("project"))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	projectVMs := schedule.Projects
	pid := schedule.ProjectID
	days := make([]schedDay, 7)
	for i := 0; i < 7; i++ {
		d := weekStart.AddDate(0, 0, i)
		days[i] = schedDay{
			Index: i, Label: fmtWeekday(resolvedLang, d), Date: d.Format("2"),
			ISO: d.Format("2006-01-02"), IsToday: sameDay(d, now),
		}
	}
	data := schedulePage{
		pageData:     pageData{Title: "Schedule", Active: "schedule", Lang: lang},
		WeekLabel:    fmtDay(resolvedLang, weekStart) + " – " + fmtDay(resolvedLang, weekStart.AddDate(0, 0, 6)),
		PrevWeek:     weekStart.AddDate(0, 0, -7).Format("2006-01-02"),
		NextWeek:     weekStart.AddDate(0, 0, 7).Format("2006-01-02"),
		ThisWeek:     weekStart.Format("2006-01-02"),
		Days:         days,
		Rows:         schedule.Rows,
		Projects:     projectVMs,
		ProjectID:    pid,
		ProjectNames: schedule.ProjectNames,
		GrandTotal:   fmtDur(r, schedule.TotalMinutes*60),
		GrandMin:     schedule.TotalMinutes,
	}
	canManageTeam := canManage(r)
	for i := range schedule.Rows {
		data.RowVMs = append(data.RowVMs, schedRowVM{Row: schedule.Rows[i], Projects: projectVMs, ProjectID: pid, CanManage: canManageTeam, Lang: lang})
	}
	s.renderPageForRequest(w, r, "Schedule", "schedule", "schedule", &data)
}

// schedProject is the project being planned: ?project=, else the first.
func schedProject(v string, projects []model.Project) int64 {
	if id, err := strconv.ParseInt(v, 10, 64); err == nil {
		for _, p := range projects {
			if p.ID == id {
				return id
			}
		}
	}
	if len(projects) > 0 {
		return projects[0].ID
	}
	return 0
}

// scheduleRows is the week per person: cells are the chosen project's
// minutes, the total and load are across all projects (a person's week is
// shared by every project), load over the five working days.
type scheduleRowsView struct {
	Rows         []schedRow
	ProjectNames map[int64]string
	Projects     []projectView
	ProjectID    int64
	TotalMinutes int
}

func (s *Server) scheduleRows(r *http.Request, weekStart time.Time, project string) (scheduleRowsView, error) {
	snapshot, err := s.services.Scheduling.List(r.Context(), teamID(r), weekStart)
	if err != nil {
		return scheduleRowsView{}, err
	}
	pid := schedProject(project, snapshot.Projects)
	now := userNow(r)
	out := make([]schedRow, 0, len(snapshot.Rows))
	for _, rc := range snapshot.Rows {
		row := schedRow{
			UserID: rc.UserID, UserName: rc.UserName, Capacity: rc.Capacity,
			TotalMin: rc.Total, Total: fmtDur(r, rc.Total*60), LoadPct: rc.LoadPercent,
		}
		proj := rc.ByProject[pid]
		for i := 0; i < 7; i++ {
			d := weekStart.AddDate(0, 0, i)
			row.Cells[i] = schedDay{
				Index: i, ISO: d.Format("2006-01-02"),
				Min: proj[i], Total: fmtDur(r, rc.Minutes[i]*60),
				IsToday: sameDay(d, now),
			}
		}
		out = append(out, row)
	}
	return scheduleRowsView{
		Rows: out, ProjectNames: snapshot.ProjectNames, Projects: projectViews(snapshot.Projects),
		ProjectID: pid, TotalMinutes: snapshot.TotalMinutes,
	}, nil
}

// handleScheduleCell writes one plan cell.
// Form: user_id, project_id, date (YYYY-MM-DD), minutes.
func (s *Server) handleScheduleCell(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	uid, err1 := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
	projectValue := strings.TrimSpace(r.PostForm.Get("project_id"))
	var pid int64
	if projectValue != "" {
		var err error
		pid, err = strconv.ParseInt(projectValue, 10, 64)
		if err != nil || pid < 0 {
			http.Error(w, "bad project_id", http.StatusBadRequest)
			return
		}
	}
	mins, err3 := strconv.Atoi(strings.TrimSpace(r.PostForm.Get("minutes")))
	day := strings.TrimSpace(r.PostForm.Get("date"))
	if err1 != nil || uid <= 0 || err3 != nil || day == "" {
		http.Error(w, "user_id, date, minutes required", 400)
		return
	}
	parsedDay, err := time.Parse("2006-01-02", day)
	if err != nil || parsedDay.Format("2006-01-02") != day {
		http.Error(w, "date must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if pid == 0 {
		// fall back to the first project so the grid always has a target
		projs, err := s.services.Projects.Queries.List(r.Context(), teamID(r), false)
		if err != nil {
			s.writeInternalError(w, err)
			return
		}
		if len(projs) == 0 {
			s.toastL(w, r, "err.needProject", "", "error")
			w.WriteHeader(200)
			return
		}
		pid = projs[0].ID
	}
	if err := s.services.Scheduling.SetCell(r.Context(), appmodel.ScheduleCellRequest{
		TeamID: teamID(r), ActorID: authenticatedUserID(r), UserID: uid, ProjectID: pid,
		Day: parsedDay, Minutes: mins,
	}); err != nil {
		if errors.Is(err, appmodel.ErrInvalidScheduleCell) {
			s.toastL(w, r, "err.invalidInput", "", "error")
		} else {
			s.logInternalError(err)
			s.toastL(w, r, "err.internal", "", "error")
		}
		w.WriteHeader(200)
		return
	}
	s.toastL(w, r, "toast.saved", "", "success")
	// Re-render the person's row.
	s.respondScheduleRow(w, r, uid, day)
}

func (s *Server) respondScheduleRow(w http.ResponseWriter, r *http.Request, uid int64, day string) {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		http.Error(w, "bad date", 400)
		return
	}
	schedule, err := s.scheduleRows(r, startOfWeek(r, t), r.PostForm.Get("project_id"))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	for _, row := range schedule.Rows {
		if row.UserID == uid {
			s.renderFragment(w, "schedule-row", schedRowVM{Row: row, Projects: schedule.Projects, ProjectID: schedule.ProjectID, CanManage: canManage(r), Lang: string(resolveLang(r))})
			return
		}
	}
	http.NotFound(w, r)
}

// schedRowVM wraps a row + the project list for the cell editor.
type schedRowVM struct {
	Row       schedRow
	Projects  []projectView
	ProjectID int64
	CanManage bool // members see the plan, managers edit it
	Lang      string
}

func (schedRowVM) isTemplateData()         {}
func (schedRowVM) isFragmentTemplateData() {}

func (v schedRowVM) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }
