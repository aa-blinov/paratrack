package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// projectsPageData is the envelope for the /projects list page.
type projectsPageData struct {
	Title        string
	Active       string
	ReactApp     bool
	Projects     []projectListRow
	ShowArchived bool
	Flash        string
	FlashOK      bool
	CSRFToken    string
	Lang         string
	// Creating a project is a manager's action, so the screen that offers it
	// must know the role; otherwise a member gets a button that answers 403.
	CanManage bool
}

func (projectsPageData) isTemplateData()    {}
func (projectsPageData) usesReactApp() bool { return true }

func (p *projectsPageData) setCSRF(t string)   { p.CSRFToken = t }
func (p *projectsPageData) setLang(l string)   { p.Lang = l }
func (p projectsPageData) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

type projectListRow struct {
	ID         int64
	Slug       string
	Name       string
	Color      string
	Archived   bool
	Activities int
	TodaySecs  int
	MonthSecs  int
	TodayLabel string // in the user's duration format
	MonthLabel string
	// ClientName is who this project is billed to. Empty when unlinked.
	ClientName string
	// RateLabel is the hourly rate in the currency the project bills in,
	// for managers; empty when unset.
	RateLabel string
	// ActivityNames are the activities behind the Activities count. The row
	// opens into them instead of asking the user to visit the project page.
	ActivityNames []projectListActivity
}

type projectListActivity struct {
	Name   string
	Color  string
	Hidden bool
}

// handleProjectsList — GET /projects?archived=1
func (s *Server) handleProjectsList(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	showArchived := r.URL.Query().Get("archived") == "1"

	now := userNow(r)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	monthFrom := now.Add(-30 * 24 * time.Hour)
	snapshot, err := s.services.Projects.Queries.ListWithUsage(r.Context(), appmodel.ProjectUsageQuery{
		Catalog:    appmodel.ProjectCatalogQuery{TeamID: tid, IncludeArchived: showArchived},
		TodayStart: dayStart, MonthStart: monthFrom, Now: now,
	})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	// Budgets resolve each project's currency (client, project, workspace),
	// so the rate reads in the money it will be billed in.
	var budgets map[int64]appmodel.ProjectBudget
	if canManage(r) {
		budgets, err = s.services.Clients.Budgets(r.Context(), appmodel.ProjectBudgetQuery{TeamID: tid})
		if err != nil {
			s.writeInternalError(w, err)
			return
		}
	}
	// Today and 30 days count a running timer as it runs, like the timesheet.
	running := map[int64][2]int{}
	for _, item := range s.runningSessions(r) {
		if id := item.Activity.ProjectID; id > 0 {
			sums := running[id]
			sums[0] += item.Session.TrackedSecondsInWindow(dayStart, now, now)
			sums[1] += item.Session.TrackedSecondsInWindow(monthFrom, now, now)
			running[id] = sums
		}
	}
	rows := make([]projectListRow, 0, len(snapshot.Projects))
	for _, p := range snapshot.Projects {
		row := projectListRow{
			ID: p.ID, Slug: p.Slug, Name: p.Name, Color: p.Color, Archived: p.Archived,
			Activities: snapshot.Usage[p.ID].ActivityCount, TodaySecs: snapshot.Usage[p.ID].TodaySeconds, MonthSecs: snapshot.Usage[p.ID].MonthSeconds,
			ClientName: snapshot.ClientNames[p.ID],
		}
		row.TodaySecs += running[p.ID][0]
		row.MonthSecs += running[p.ID][1]
		row.TodayLabel, row.MonthLabel = fmtDur(r, row.TodaySecs), fmtDur(r, row.MonthSecs)
		if budget, ok := budgets[p.ID]; ok && p.BillableRateCents != nil {
			lang := resolveLang(r)
			row.RateLabel = moneyL(lang, *p.BillableRateCents, budget.Currency) + " / " + i18n.T(lang, "inv.hoursShort")
		}
		for _, a := range snapshot.ActivitiesByProject[p.ID] {
			row.ActivityNames = append(row.ActivityNames, projectListActivity{Name: a.Name, Color: a.Color, Hidden: a.Hidden})
		}
		rows = append(rows, row)
	}

	data := projectsPageData{
		Title:        "Projects",
		Active:       "projects",
		ReactApp:     true,
		Projects:     rows,
		ShowArchived: showArchived,
		CanManage:    canManage(r),
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Projects", "projects", &data)
}

// handleProjectDetail — GET /projects/{slug}
func (s *Server) handleProjectDetail(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	slug := strings.TrimPrefix(r.URL.Path, "/projects/")
	if i := strings.IndexByte(slug, '/'); i >= 0 {
		slug = slug[:i]
	}
	now := userNow(r)
	from := now.Add(-30 * 24 * time.Hour)
	snapshot, err := s.services.ProjectPages.Build(r.Context(), appmodel.ProjectPageRequest{
		TeamID: tid, CallerID: authenticatedUserID(r), Slug: slug,
		IncludeArchived: r.URL.Query().Get("archived") == "1",
		From:            from, Through: now,
		IncludeUnbilled: s.teamModules(r)["invoices"],
	})
	if err != nil {
		s.writeProjectLookupError(w, r, err)
		return
	}
	extras, err := s.buildProjectExtras(r, snapshot.Detail.Project)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data := s.buildProjectDetailPage(r, snapshot, extras)
	plan, err := s.projectPlan(r, snapshot.Detail.Project)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data.People = plan.People
	data.PlannedLabel = plan.PlannedLabel
	data.RemainingLabel = plan.RemainingLabel
	data.RemainingOver = plan.RemainingOver
	data.LeadLabel = plan.LeadLabel
	data.DeadlineInput = snapshot.Detail.Project.Deadline
	data.DeadlineLabel = deadlineLabel(r, snapshot.Detail.Project.Deadline)
	data.DeadlineNote, data.DeadlineOverdue = deadlineNote(r, snapshot.Detail.Project.Deadline)
	data.LeadInput = ""
	if snapshot.Detail.Project.LeadUserID > 0 {
		data.LeadInput = strconv.FormatInt(snapshot.Detail.Project.LeadUserID, 10)
	}
	data.MemberOptions = s.memberOptions(r)
	// This page never read a flash before. Creating redirects here, so the
	// "created" confirmation had nowhere to render: the query arrived and was
	// dropped. Every other page that redirects to itself reads it.
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, data.Title, "projects", &data)
}

// projectNewPage is the create form envelope. T() exposes i18n.
type projectNewPage struct {
	Title        string
	Active       string
	ReactApp     bool
	NewProject   bool
	CSRFToken    string
	Lang         string
	Currencies   []currencyOption
	TeamCurrency string
	// SuggestedColor — the first palette colour this team is not already
	// using. The form opens on it so a project is distinguishable from the
	// rest before anyone touches the picker.
	SuggestedColor string
	// MemberOptions are the people a new project can be led by.
	MemberOptions []memberOption
}

func (projectNewPage) isTemplateData()    {}
func (projectNewPage) usesReactApp() bool { return true }

func (p projectNewPage) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

func (p *projectNewPage) setCSRF(token string) { p.CSRFToken = token }
func (p *projectNewPage) setLang(lang string)  { p.Lang = lang }

// handleProjectNew — GET /projects/new (form page).
func (s *Server) handleProjectNew(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := projectNewPage{Title: "New project", Active: "projects", ReactApp: true, NewProject: true, CSRFToken: ensureCSRF(w, r), Lang: lang,
		Currencies: currencyOptions(), MemberOptions: s.memberOptions(r)}
	var err error
	data.TeamCurrency, err = s.services.Teams.Settings.Currency(r.Context(), teamID(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data.SuggestedColor, err = s.suggestedProjectColor(r)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.renderPageForRequest(w, r, "New project", "projects", &data)
}

// suggestedProjectColor — the first palette colour no project in this team
// already wears. A failure to read the catalogue must not block the form, so
// the palette's first colour is the floor.
func (s *Server) suggestedProjectColor(r *http.Request) (string, error) {
	list, err := s.services.Projects.Queries.List(r.Context(), appmodel.ProjectCatalogQuery{TeamID: teamID(r)})
	if err != nil {
		return projectPalette[0], nil
	}
	used := make([]string, 0, len(list))
	for _, project := range list {
		used = append(used, project.Color)
	}
	return nextProjectColor(used), nil
}

// handleProjectCreateForm — POST /projects/new (form-encoded from the
// create page). Redirects to /projects/{slug} on success.
func (s *Server) handleProjectCreateForm(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	rate, hasRate, rerr := formCents(r, "rate")
	if rerr != nil {
		http.Redirect(w, r, "/projects?flash="+encodeFlash(false, i18n.T(resolveLang(r), "bill.badRate")), http.StatusSeeOther)
		return
	}
	currency := r.Form.Get("currency")
	if currency != "" && !validCurrency(currency) {
		http.Redirect(w, r, "/projects?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.bad currency")), http.StatusSeeOther)
		return
	}
	var ratePtr *int
	if hasRate {
		ratePtr = &rate
	}
	deadline := strings.TrimSpace(r.Form.Get("deadline"))
	if _, err := time.Parse("2006-01-02", deadline); deadline != "" && err != nil {
		http.Redirect(w, r, "/projects?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.bad deadline")), http.StatusSeeOther)
		return
	}
	lead, leadErr := strconv.ParseInt(strings.TrimSpace(r.Form.Get("lead_user_id")), 10, 64)
	if leadErr != nil || lead < 0 {
		lead = 0
	}
	p, err := s.services.Projects.Commands.Create(r.Context(), appmodel.ProjectCreateRequest{
		TeamID: tid, CallerID: authenticatedUserID(r), Name: r.Form.Get("name"),
		Slug: r.Form.Get("slug"), Color: r.Form.Get("color"), RateCents: ratePtr, Currency: currency,
		Deadline: deadline, LeadUserID: lead,
	})
	if err != nil {
		flash := encodeFlash(false, s.projectFlashError(err, "create"))
		http.Redirect(w, r, "/projects?flash="+flash, http.StatusSeeOther)
		return
	}
	// Errors have always flashed here; success went silent, so a person who
	// created a project could not tell a saved one from a failed one.
	//
	// The flash travels as a code, not as text — every other flash does this,
	// and it is not a convention for tidiness: http.Redirect percent-encodes
	// non-ASCII but leaves a raw space, and a space in the query makes the
	// request 400. Measured: passing the Russian message directly produced
	// "/projects/client-work?flash=%d0%9f…%20%d1%81…" and a 400 Bad Request.
	// A code is ASCII, and the page translates it in the reader's language.
	http.Redirect(w, r, "/projects/"+p.Slug+"?flash="+encodeFlash(true, "project_created"), http.StatusSeeOther)
}

// handleProjectUpdateForm — POST /projects/{slug} (rename / color / archive).
func (s *Server) handleProjectUpdateForm(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	slug := strings.TrimPrefix(r.URL.Path, "/projects/")
	if i := strings.IndexByte(slug, '/'); i >= 0 {
		slug = slug[:i]
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	update := appmodel.ProjectUpdate{Name: r.Form.Get("name"), Color: r.Form.Get("color")}
	archived := r.Form.Get("archived") == "1" || r.Form.Get("archived") == "true"
	update.Archived = &archived
	if v := strings.TrimSpace(r.Form.Get("estimate_minutes")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			flash := encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.estimate must be a number of minutes"))
			http.Redirect(w, r, "/projects/"+slug+"?flash="+flash, http.StatusSeeOther)
			return
		}
		update.EstimateMinutes = &n
	}
	rate, hasRate, err := formCents(r, "rate")
	if err != nil {
		http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "bill.badRate")), http.StatusSeeOther)
		return
	}
	if hasRate {
		update.RateCents = &rate
	}
	billable := r.Form.Get("billable") == "1" || r.Form.Get("billable") == "on" || r.Form.Get("billable") == "true"
	update.Billable = &billable
	// The client picker is always present in the project form, so an empty
	// value means "detach" rather than "leave as is".
	if _, sent := r.Form["client_id"]; sent {
		clientID, convErr := strconv.ParseInt(strings.TrimSpace(r.Form.Get("client_id")), 10, 64)
		if convErr != nil || clientID < 0 {
			http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.bad client")), http.StatusSeeOther)
			return
		}
		update.ClientID = &clientID
	}
	// A present-but-empty budget field removes the ceiling (see db layer);
	// an absent field means the form never showed one, so nothing changes.
	// Through the same parser as the hourly rate, because the field comes
	// back pre-filled with a grouped amount ("600 000,00"): reading it as a
	// plain decimal made every second save of a project fail with a complaint
	// about the rate, which had not changed at all.
	if _, sent := r.Form["deadline"]; sent {
		day := strings.TrimSpace(r.Form.Get("deadline"))
		if _, err := time.Parse("2006-01-02", day); day != "" && err != nil {
			http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.bad deadline")), http.StatusSeeOther)
			return
		}
		update.Deadline = &day
	}
	if _, sent := r.Form["lead_user_id"]; sent {
		// An empty value is "no lead": the form posts it when the picker says so.
		lead := int64(0)
		raw := strings.TrimSpace(r.Form.Get("lead_user_id"))
		parsed, convErr := strconv.ParseInt(raw, 10, 64)
		if raw != "" {
			lead = parsed
		}
		if raw != "" && (convErr != nil || lead < 0) {
			http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.bad lead")), http.StatusSeeOther)
			return
		}
		update.LeadUserID = &lead
	}
	if _, sent := r.Form["budget"]; sent {
		raw := strings.TrimSpace(r.Form.Get("budget"))
		if raw == "" {
			zero := 0
			update.BudgetCents = &zero
		} else {
			cents, hasBudget, parseErr := formCents(r, "budget")
			if parseErr != nil || !hasBudget {
				http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "budget.badAmount")), http.StatusSeeOther)
				return
			}
			update.BudgetCents = &cents
		}
	}
	if _, sent := r.Form["currency"]; sent {
		cur := r.Form.Get("currency")
		if cur != "" && !validCurrency(cur) {
			http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.bad currency")), http.StatusSeeOther)
			return
		}
		update.Currency = &cur
	}
	if _, err := s.services.Projects.Commands.UpdateBySlug(r.Context(), appmodel.ProjectSlugUpdateRequest{
		TeamID: tid, Slug: slug, CallerID: authenticatedUserID(r), Update: update,
	}); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			s.writeProjectLookupError(w, r, err)
			return
		}
		http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, s.projectFlashError(err, "update")), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(true, "updated"), http.StatusSeeOther)
}

// handleProjectDeleteForm — POST /projects/{slug}/delete.
func (s *Server) handleProjectDeleteForm(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	slug := strings.TrimPrefix(r.URL.Path, "/projects/")
	slug = strings.TrimSuffix(slug, "/delete")
	if err := s.services.Projects.Commands.DeleteBySlug(r.Context(), appmodel.ProjectSlugMutationRequest{
		TeamID: tid, Slug: slug, CallerID: authenticatedUserID(r),
	}); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			s.writeProjectLookupError(w, r, err)
			return
		}
		flash := encodeFlash(false, s.projectFlashError(err, "delete"))
		http.Redirect(w, r, "/projects?flash="+flash, http.StatusSeeOther)
		return
	}
	flash := encodeFlash(true, i18n.T(resolveLang(r), "flash.msg.project deleted"))
	http.Redirect(w, r, "/projects?flash="+flash, http.StatusSeeOther)
}

func (s *Server) writeProjectLookupError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, model.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	s.writeInternalError(w, err)
}

func (s *Server) projectFlashError(err error, action string) string {
	switch {
	case errors.Is(err, model.ErrAlreadyExists):
		return "a project with that slug or name already exists"
	case errors.Is(err, model.ErrNotFound):
		return "project not found"
	case errors.Is(err, model.ErrForbidden):
		return "manager role required"
	case isProjectInputError(err):
		return err.Error()
	default:
		s.logInternalError(err)
		return "could not " + action + " project"
	}
}

// handleProjectRate saves the billable rate for a project.
// Form: slug, rate_cents, billable (1/0).
func (s *Server) handleProjectRate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/projects/"+url.PathEscape(r.PathValue("slug"))+"?flash=bad_request", http.StatusSeeOther)
		return
	}
	slug := strings.TrimSpace(r.PostForm.Get("slug"))
	p, err := s.services.Projects.Queries.GetBySlug(r.Context(), appmodel.ProjectSlugQuery{TeamID: teamID(r), Slug: slug})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var rate *int
	if n, has, err := formCents(r, "rate"); err != nil {
		http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "bill.badRate")),
			http.StatusSeeOther)
		return
	} else if has {
		rate = &n
	}
	var billable *bool
	if v := strings.TrimSpace(r.PostForm.Get("billable")); v != "" {
		b := v == "1" || v == "true" || v == "on"
		billable = &b
	}
	caller, ok := UserFrom(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.services.Projects.Commands.UpdateRate(r.Context(), appmodel.ProjectRateRequest{TeamID: teamID(r), ProjectID: p.ID, CallerID: caller.ID, RateCents: rate, Billable: billable}); err != nil {
		if errors.Is(err, model.ErrForbidden) {
			http.Redirect(w, r, "/projects/"+url.PathEscape(slug)+"?flash=forbidden", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/projects/"+url.PathEscape(slug)+"?flash="+encodeFlash(false, s.projectFlashError(err, "update")), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(true, "updated"), http.StatusSeeOther)
}
