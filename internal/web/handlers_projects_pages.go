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
	Projects     []projectListRow
	ShowArchived bool
	Flash        string
	FlashOK      bool
	CSRFToken    string
	Lang         string
}

func (projectsPageData) isTemplateData() {}

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
	rows := make([]projectListRow, 0, len(snapshot.Projects))
	for _, p := range snapshot.Projects {
		row := projectListRow{
			ID: p.ID, Slug: p.Slug, Name: p.Name, Color: p.Color, Archived: p.Archived,
			Activities: snapshot.Usage[p.ID].ActivityCount, TodaySecs: snapshot.Usage[p.ID].TodaySeconds, MonthSecs: snapshot.Usage[p.ID].MonthSeconds,
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
	data := s.buildProjectDetailPage(r, snapshot)
	s.renderPageForRequest(w, r, data.Title, "projects", "project-detail", &data)
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

func (projectNewPage) isTemplateData() {}

func (p projectNewPage) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

func (p *projectNewPage) setCSRF(token string) { p.CSRFToken = token }
func (p *projectNewPage) setLang(lang string)  { p.Lang = lang }

// handleProjectNew — GET /projects/new (form page).
func (s *Server) handleProjectNew(w http.ResponseWriter, r *http.Request) {
	lang := string(resolveLang(r))
	data := projectNewPage{Title: "New project", Active: "projects", CSRFToken: ensureCSRF(w, r), Lang: lang,
		Currencies: currencyOptions()}
	var err error
	data.TeamCurrency, err = s.services.Teams.Settings.Currency(r.Context(), teamID(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.renderPageForRequest(w, r, "New project", "projects", "project-new", &data)
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
		http.Redirect(w, r, "/projects?flash="+encodeFlash(false, "bad currency"), http.StatusSeeOther)
		return
	}
	var ratePtr *int
	if hasRate {
		ratePtr = &rate
	}
	p, err := s.services.Projects.Commands.Create(r.Context(), appmodel.ProjectCreateRequest{
		TeamID: tid, CallerID: authenticatedUserID(r), Name: r.Form.Get("name"),
		Slug: r.Form.Get("slug"), Color: r.Form.Get("color"), RateCents: ratePtr, Currency: currency,
	})
	if err != nil {
		flash := encodeFlash(false, s.projectFlashError(err, "create"))
		http.Redirect(w, r, "/projects?flash="+flash, http.StatusSeeOther)
		return
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
			flash := encodeFlash(false, "estimate must be a number of minutes")
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
	if _, sent := r.Form["currency"]; sent {
		cur := r.Form.Get("currency")
		if cur != "" && !validCurrency(cur) {
			http.Redirect(w, r, "/projects/"+slug+"?flash="+encodeFlash(false, "bad currency"), http.StatusSeeOther)
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
	flash := encodeFlash(true, "project deleted")
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
	p, err := s.services.Projects.Queries.GetBySlug(r.Context(), teamID(r), slug)
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
