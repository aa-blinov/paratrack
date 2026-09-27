package web

import (
	dbpkg "github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/teams"
)

// ---------------------------------------------------------------------------
// Wave 6: payroll + resource scheduling
// ---------------------------------------------------------------------------

// handlePayroll lists runs and offers a generator.
func (s *Server) handlePayroll(w http.ResponseWriter, r *http.Request) {
	list, _ := s.db.ListPayrollRuns(r.Context(), teamID(r))
	lang := string(resolveLang(r))
	data := payrollPage{pageData: pageData{Title: "Payroll", Active: "payroll", Lang: lang}}
	for _, run := range list {
		lines, _ := s.db.ListPayrollLines(r.Context(), run.ID)
		total, secs := 0, 0
		for _, l := range lines {
			total += l.AmountCents
			secs += dbpkg.HoursHundredths(l.Seconds)
		}
		data.Items = append(data.Items, payrollSummary{
			ID: run.ID, Number: run.Number, Status: run.Status,
			Total: moneyL(resolveLang(r), total, run.Currency), Hours: fmtHoursL(resolveLang(r), secs),
			Period: fmtDay(resolveLang(r), run.PeriodStart) + " – " + fmtDay(resolveLang(r), run.PeriodEnd.AddDate(0, 0, -1)),
		})
	}
	now := userNow(r)
	data.DefStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	data.DefEnd = now.Format("2006-01-02")
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Payroll", "payroll", "payroll", &data)
}

type payrollSummary struct {
	ID     int64
	Number string
	Status string
	Total  string
	Hours  string
	Period string
}

type payrollPage struct {
	pageData
	Items    []payrollSummary
	DefStart string
	DefEnd   string
	Flash    string
	FlashOK  bool
}

func (p *payrollPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handlePayrollCreate generates a pay run from tracked time.
func (s *Server) handlePayrollCreate(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	startStr := strings.TrimSpace(r.PostForm.Get("start"))
	endStr := strings.TrimSpace(r.PostForm.Get("end"))
	notes := strings.TrimSpace(r.PostForm.Get("notes"))
	start, err1 := time.ParseInLocation("2006-01-02", startStr, userLoc(r))
	end, err2 := time.ParseInLocation("2006-01-02", endStr, userLoc(r))
	if err1 != nil || err2 != nil || end.Before(start) {
		http.Redirect(w, r, "/payroll?flash="+encodeFlash(false, "bad period"), http.StatusSeeOther)
		return
	}
	end = end.AddDate(0, 0, 1)
	lines, err := s.db.BuildPayrollLines(r.Context(), teamID(r), start, end)
	if err != nil {
		http.Redirect(w, r, "/payroll?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	if len(lines) == 0 {
		http.Redirect(w, r, "/payroll?flash="+encodeFlash(false, "no paid members with tracked time in that period"), http.StatusSeeOther)
		return
	}
	number, err := s.db.NextPayrollNumber(r.Context(), teamID(r))
	if err != nil {
		http.Redirect(w, r, "/payroll?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	run, err := s.db.CreatePayrollRun(r.Context(), teamID(r), number, notes, start, end, lines)
	if err != nil {
		http.Redirect(w, r, "/payroll?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	s.audit(r, "payroll.create", run.Number, "")
	http.Redirect(w, r, "/payroll/"+strconv.FormatInt(run.ID, 10), http.StatusSeeOther)
}

// handlePayrollDetail renders one run.
func (s *Server) handlePayrollDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/payroll/"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	run, err := s.db.GetPayrollRun(r.Context(), teamID(r), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	lines, _ := s.db.ListPayrollLines(r.Context(), run.ID)
	lang := string(resolveLang(r))
	total, secs := 0, 0
	vms := make([]payrollLineVM, 0, len(lines))
	for _, l := range lines {
		total += l.AmountCents
		secs += dbpkg.HoursHundredths(l.Seconds)
		vms = append(vms, payrollLineVM{
			Label: l.Label, Hours: fmtHoursL(resolveLang(r), dbpkg.HoursHundredths(l.Seconds)),
			Rate: moneyL(resolveLang(r), l.RateCents, run.Currency), Amount: moneyL(resolveLang(r), l.AmountCents, run.Currency),
		})
	}
	data := payrollDetailPage{
		pageData: pageData{Title: run.Number, Active: "payroll", Lang: lang},
		Run: payrollVM{
			ID: run.ID, Number: run.Number, Status: run.Status, Notes: run.Notes,
			PeriodLabel: fmtDate(resolveLang(r), run.PeriodStart) + " – " + fmtDate(resolveLang(r), run.PeriodEnd.AddDate(0, 0, -1)),
			Lines:       vms, Total: moneyL(resolveLang(r), total, run.Currency), TotalCents: total, Hours: fmtHoursL(resolveLang(r), secs),
		},
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, run.Number, "payroll", "payroll-detail", &data)
}

type payrollLineVM struct {
	Label  string
	Hours  string
	Rate   string
	Amount string
}

type payrollVM struct {
	ID          int64
	Number      string
	Status      string
	Notes       string
	PeriodLabel string
	Lines       []payrollLineVM
	Total       string
	TotalCents  int
	Hours       string
}

type payrollDetailPage struct {
	pageData
	Run     payrollVM
	Flash   string
	FlashOK bool
}

func (p *payrollDetailPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handlePayrollPaid marks a run paid.
func (s *Server) handlePayrollPaid(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = s.db.MarkPayrollPaid(r.Context(), teamID(r), id)
	s.audit(r, "payroll.paid", strconv.FormatInt(id, 10), "")
	// The people on the run hear their pay went out (the run page is for
	// managers, so the link is their own stats).
	var paid []int64
	if lines, err := s.db.ListPayrollLines(r.Context(), id); err == nil {
		for _, l := range lines {
			paid = append(paid, l.UserID)
		}
	}
	s.sendPush(teamID(r), paid, "Payroll paid", "Pay run marked paid", "/stats")
	http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(true, "marked paid"),
		http.StatusSeeOther)
}

// handlePayrollDelete removes a draft run.
func (s *Server) handlePayrollDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = s.db.DeletePayrollRun(r.Context(), teamID(r), id)
	http.Redirect(w, r, "/payroll?flash=removed", http.StatusSeeOther)
}

// handleMemberPay saves a member's pay rate + daily capacity.
func (s *Server) handleMemberPay(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	uid, err := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, "bad user"), http.StatusSeeOther)
		return
	}
	// The owner's own pay is the owner's to set.
	if role, _, _ := s.teams.IsMember(r.Context(), teamID(r), uid); role == teams.RoleOwner && RoleFrom(r.Context()) != teams.RoleOwner {
		http.Redirect(w, r, "/settings/members?flash=forbidden", http.StatusSeeOther)
		return
	}
	var pay, cap *int
	if n, has, err := formCents(r, "hourly_pay"); err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, i18n.T(resolveLang(r), "bill.badRate")), http.StatusSeeOther)
		return
	} else if has {
		pay = &n
	}
	if v := strings.TrimSpace(r.PostForm.Get("capacity_minutes")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, "bad capacity"), http.StatusSeeOther)
			return
		}
		cap = &n
	}
	if err := s.db.SetMemberPay(r.Context(), teamID(r), uid, pay, cap); err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	s.audit(r, "member.pay_update", strconv.FormatInt(uid, 10), "")
	http.Redirect(w, r, "/settings/members?flash="+encodeFlash(true, "updated"), http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Resource scheduling (/schedule)
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
	Projects     []model.Project
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
	projects, _ := s.db.ListProjects(r.Context(), teamID(r), false)
	pid := schedProject(r.URL.Query().Get("project"), projects)
	srows, pnames, err := s.scheduleRows(r, weekStart, pid)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	days := make([]schedDay, 7)
	for i := 0; i < 7; i++ {
		d := weekStart.AddDate(0, 0, i)
		days[i] = schedDay{
			Index: i, Label: fmtWeekday(resolveLang(r), d), Date: d.Format("2"),
			ISO: d.Format("2006-01-02"), IsToday: sameDay(d, now),
		}
	}
	grand := 0
	for _, row := range srows {
		grand += row.TotalMin
	}
	data := schedulePage{
		pageData:     pageData{Title: "Schedule", Active: "schedule", Lang: string(resolveLang(r))},
		WeekLabel:    fmtDay(resolveLang(r), weekStart) + " – " + fmtDay(resolveLang(r), weekStart.AddDate(0, 0, 6)),
		PrevWeek:     weekStart.AddDate(0, 0, -7).Format("2006-01-02"),
		NextWeek:     weekStart.AddDate(0, 0, 7).Format("2006-01-02"),
		ThisWeek:     weekStart.Format("2006-01-02"),
		Days:         days,
		Rows:         srows,
		Projects:     projects,
		ProjectID:    pid,
		ProjectNames: pnames,
		GrandTotal:   fmtDur(r, grand*60),
		GrandMin:     grand,
	}
	for i := range srows {
		data.RowVMs = append(data.RowVMs, schedRowVM{Row: srows[i], Projects: projects, ProjectID: pid, CanManage: canManage(r), Lang: string(resolveLang(r))})
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
func (s *Server) scheduleRows(r *http.Request, weekStart time.Time, pid int64) ([]schedRow, map[int64]string, error) {
	rows, pnames, err := s.db.ListSchedule(r.Context(), teamID(r), weekStart)
	if err != nil {
		return nil, nil, err
	}
	now := userNow(r)
	out := make([]schedRow, 0, len(rows))
	for _, rc := range rows {
		row := schedRow{
			UserID: rc.UserID, UserName: rc.UserName, Capacity: rc.Capacity,
			TotalMin: rc.Total, Total: fmtDur(r, rc.Total*60),
		}
		if capWeek := rc.Capacity * 5; capWeek > 0 {
			row.LoadPct = rc.Total * 100 / capWeek
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
	return out, pnames, nil
}

// handleScheduleCell writes one plan cell.
// Form: user_id, project_id, date (YYYY-MM-DD), minutes.
func (s *Server) handleScheduleCell(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	uid, err1 := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
	pid, _ := strconv.ParseInt(r.PostForm.Get("project_id"), 10, 64)
	mins, err3 := strconv.Atoi(strings.TrimSpace(r.PostForm.Get("minutes")))
	day := strings.TrimSpace(r.PostForm.Get("date"))
	if err1 != nil || err3 != nil || day == "" {
		http.Error(w, "user_id, date, minutes required", 400)
		return
	}
	if pid == 0 {
		// fall back to the first project so the grid always has a target
		projs, _ := s.db.ListProjects(r.Context(), teamID(r), false)
		if len(projs) == 0 {
			s.toastL(w, r, "err.needProject", "", "error")
			w.WriteHeader(200)
			return
		}
		pid = projs[0].ID
	}
	if err := s.db.UpsertScheduleEntry(r.Context(), teamID(r), uid, pid, day, mins, ""); err != nil {
		s.toast(w, err.Error(), "error")
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
	projects, _ := s.db.ListProjects(r.Context(), teamID(r), false)
	pid := schedProject(r.PostForm.Get("project_id"), projects)
	rows, _, err := s.scheduleRows(r, startOfWeek(r, t), pid)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for _, row := range rows {
		if row.UserID == uid {
			s.renderFragment(w, "schedule-row", schedRowVM{Row: row, Projects: projects, ProjectID: pid, CanManage: canManage(r), Lang: string(resolveLang(r))})
			return
		}
	}
	http.NotFound(w, r)
}

// schedRowVM wraps a row + the project list for the cell editor.
type schedRowVM struct {
	Row       schedRow
	Projects  []model.Project
	ProjectID int64
	CanManage bool // members see the plan, managers edit it
	Lang      string
}

func (v schedRowVM) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }
