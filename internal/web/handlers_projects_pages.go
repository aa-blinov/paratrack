package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// projectsPageData is the envelope for the /projects list page.
type projectsPageData struct {
	Title    string
	Active   string
	Projects []projectListRow
	ShowArchived bool
	Flash    string
	FlashOK  bool
	CSRFToken string
	Lang      string
}

func (p *projectsPageData) setCSRF(t string) { p.CSRFToken = t }
func (p *projectsPageData) setLang(l string) { p.Lang = l }
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
}

// handleProjectsList — GET /projects?archived=1
func (s *Server) handleProjectsList(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	showArchived := r.URL.Query().Get("archived") == "1"

	projects, err := s.db.ListProjects(r.Context(), tid, showArchived)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	now := userNow(r)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	monthFrom := now.Add(-30 * 24 * time.Hour)
	counts, _ := s.db.ProjectActivityCounts(r.Context(), tid)
	spans, _ := s.db.ProjectSpans(r.Context(), tid, monthFrom, now)
	today, month := map[int64]int{}, map[int64]int{}
	for _, sp := range spans {
		// "Сегодня" is today from midnight, not the last 24 h.
		today[sp.ProjectID] += sp.Session.TrackedSecondsInWindow(dayStart, now, now)
		month[sp.ProjectID] += sp.Session.TrackedSecondsInWindow(monthFrom, now, now)
	}
	rows := make([]projectListRow, 0, len(projects))
	for _, p := range projects {
		row := projectListRow{
			ID: p.ID, Slug: p.Slug, Name: p.Name, Color: p.Color, Archived: p.Archived,
			Activities: counts[p.ID], TodaySecs: today[p.ID], MonthSecs: month[p.ID],
		}
		row.TodayLabel, row.MonthLabel = fmtDur(r, row.TodaySecs), fmtDur(r, row.MonthSecs)
		rows = append(rows, row)
	}

	data := projectsPageData{
		Title:        "Projects",
		Active:       "projects",
		Projects:     rows,
		ShowArchived: showArchived,
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Projects", "projects", "projects", &data)
}

// projectDetailData is the envelope for /projects/{slug}.
type projectDetailData struct {
	Title    string
	Active   string
	Project  model.Project
	Activities []model.Activity
	Sessions []sessionView
	Total    string
	MonthTotal string
	Archived bool
	EstimateLabel   string
	EstimateInput   string
	EstimatePercent int
	RateInput       string
	Currency        string // project's own ('' = workspace's)
	TeamCurrency    string
	Currencies      []currencyOption
	Flash    string
	FlashOK  bool
	CSRFToken string
	Lang      string
	Unbilled  []unbilledView
	CanManage bool // rates, client and settings are for managers only
}

func (p *projectDetailData) setCSRF(t string) { p.CSRFToken = t }
func (p *projectDetailData) setManage(v bool) { p.CanManage = v }
func (p *projectDetailData) setLang(l string) { p.Lang = l }
func (p projectDetailData) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

// handleProjectDetail — GET /projects/{slug}
func (s *Server) handleProjectDetail(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	slug := strings.TrimPrefix(r.URL.Path, "/projects/")
	if i := strings.IndexByte(slug, '/'); i >= 0 {
		slug = slug[:i]
	}
	p, err := s.db.GetProjectBySlug(r.Context(), tid, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	includeArchived := r.URL.Query().Get("archived") == "1"
	acts, err := s.db.ListActivitiesForProject(r.Context(), p.ID, includeArchived)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Recent sessions in this project (last 30 days, capped at 50).
	now := userNow(r)
	from := now.Add(-30 * 24 * time.Hour)
	rawSessions, err := s.db.ProjectSessions(r.Context(), tid, p.ID, from, now)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views := make([]sessionView, 0, min(len(rawSessions), 50))
	monthSec := 0
	for _, as := range rawSessions {
		v := toSessionView(as.Session, as.Activity, from, now, now, resolveLang(r), durFmtOf(r))
		monthSec += v.DurationSecs
		if len(views) < 50 {
			views = append(views, v)
		}
	}
	// "За всё время" is the whole history, tracked time (pauses out).
	totalSec, _ := s.db.ProjectTrackedTotal(r.Context(), tid, p.ID)
	hydrateSessionTags(r.Context(), s.db, views)
	hydrateSessionProjects(r.Context(), s.db, views)

	estLabel := ""
	estInput := ""
	estPct := 0
	rateInput := ""
	if p.EstimateMinutes != nil && *p.EstimateMinutes > 0 {
		estLabel = fmtDur(r, *p.EstimateMinutes * 60)
		estInput = strconv.Itoa(*p.EstimateMinutes)
		estPct = totalSec * 100 / (*p.EstimateMinutes * 60)
	}
	if p.BillableRateCents != nil && canManage(r) {
		rateInput = formatMoneyInput(resolveLang(r), *p.BillableRateCents)
	}
	data := projectDetailData{
		Title:      p.Name,
		Active:     "projects",
		Project:    p,
		Activities: acts,
		Sessions:   views,
		Total:      fmtDur(r, totalSec),
		MonthTotal: fmtDur(r, monthSec),
		Archived:   p.Archived,
		EstimateLabel:   estLabel,
		EstimateInput:   estInput,
		RateInput:       rateInput,
		EstimatePercent: estPct,
		Currencies:      currencyOptions(),
	}
	data.Currency, _ = s.db.ProjectCurrency(r.Context(), teamID(r), p.ID)
	if canManage(r) && s.teamModules(r)["invoices"] {
		data.Unbilled = s.unbilledViews(r, p.ID)
	}
	data.TeamCurrency, _ = s.db.TeamCurrency(r.Context(), teamID(r))
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, p.Name, "projects", "project-detail", &data)
}

// projectNewPage is the create form envelope. T() exposes i18n.
type projectNewPage struct {
	Title        string
	Active       string
	CSRFToken    string
	Lang         string
	Currencies   []currencyOption
	TeamCurrency string
}

func (p projectNewPage) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

// handleProjectNew — GET /projects/new (form page).
func (s *Server) handleProjectNew(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := projectNewPage{Title: "New project", Active: "projects", CSRFToken: ensureCSRF(w, r), Lang: lang,
		Currencies: currencyOptions()}
	data.TeamCurrency, _ = s.db.TeamCurrency(r.Context(), teamID(r))
	s.renderPageForRequest(w, r, "New project", "projects", "project-new", &data)
}

// handleProjectCreateForm — POST /projects/new (form-encoded from the
// create page). Redirects to /projects/{slug} on success.
func (s *Server) handleProjectCreateForm(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	rate, hasRate, rerr := formCents(r, "rate")
	if rerr != nil {
		http.Redirect(w, r, "/projects?flash="+encodeFlash(false, i18n.T(resolveLang(r), "bill.badRate")), http.StatusSeeOther)
		return
	}
	p, err := s.db.CreateProject(r.Context(), tid,
		r.Form.Get("name"), r.Form.Get("slug"), r.Form.Get("color"))
	if err != nil {
		flash := encodeFlash(false, err.Error())
		http.Redirect(w, r, "/projects?flash="+flash, http.StatusSeeOther)
		return
	}
	// Rate and currency right away: an invoice needs them, and the edit
	// card is where nobody looks.
	if hasRate {
		_ = s.db.SetProjectRate(r.Context(), tid, p.ID, &rate, nil)
	}
	if cur := r.Form.Get("currency"); validCurrency(cur) {
		_ = s.db.SetProjectCurrency(r.Context(), tid, p.ID, cur)
	}
	http.Redirect(w, r, "/projects/"+p.Slug, http.StatusSeeOther)
}

// handleProjectUpdateForm — POST /projects/{slug} (rename / color / archive).
func (s *Server) handleProjectUpdateForm(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	slug := strings.TrimPrefix(r.URL.Path, "/projects/")
	if i := strings.IndexByte(slug, '/'); i >= 0 {
		slug = slug[:i]
	}
	p, err := s.db.GetProjectBySlug(r.Context(), tid, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := r.Form.Get("name")
	color := r.Form.Get("color")
	var archived *bool
	if v := r.Form.Get("archived"); v != "" {
		b := v == "1" || v == "true"
		archived = &b
	}
	var estimate *int
	if v := strings.TrimSpace(r.Form.Get("estimate_minutes")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			flash := encodeFlash(false, "estimate must be a number of minutes")
			http.Redirect(w, r, "/projects/"+slug+"?flash="+flash, http.StatusSeeOther)
			return
		}
		estimate = &n
	}
	if _, err := s.db.UpdateProject(r.Context(), tid, p.ID, name, color, archived, estimate); err != nil {
		http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
		return
	}
	// Billable rate + flag (Wave 3).
	if n, has, err := formCents(r, "rate"); has || r.Form.Get("billable") != "" {
		var rate *int
		if err != nil {
			http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "bill.badRate")),
				http.StatusSeeOther)
			return
		}
		if has {
			rate = &n
		}
		b := r.Form.Get("billable") == "1" || r.Form.Get("billable") == "on"
		if err := s.db.SetProjectRate(r.Context(), tid, p.ID, rate, &b); err != nil {
			http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, err.Error()), http.StatusSeeOther)
			return
		}
	}
	// Rate currency: '' follows the workspace.
	if _, sent := r.Form["currency"]; sent {
		if cur := r.Form.Get("currency"); cur == "" || validCurrency(cur) {
			_ = s.db.SetProjectCurrency(r.Context(), tid, p.ID, cur)
		}
	}
	http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(true, "updated"), http.StatusSeeOther)
}

// handleProjectDeleteForm — POST /projects/{slug}/delete.
func (s *Server) handleProjectDeleteForm(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	slug := strings.TrimPrefix(r.URL.Path, "/projects/")
	slug = strings.TrimSuffix(slug, "/delete")
	p, err := s.db.GetProjectBySlug(r.Context(), tid, slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.db.DeleteProject(r.Context(), tid, p.ID); err != nil {
		flash := encodeFlash(false, err.Error())
		http.Redirect(w, r, "/projects?flash="+flash, http.StatusSeeOther)
		return
	}
	flash := encodeFlash(true, "project deleted")
	http.Redirect(w, r, "/projects?flash="+flash, http.StatusSeeOther)
}


