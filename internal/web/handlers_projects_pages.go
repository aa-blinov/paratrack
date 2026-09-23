package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/db"
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
}

type projectListRow struct {
	ID         int64
	Slug       string
	Name       string
	Color      string
	Archived   bool
	Activities int
	TodaySecs  int
	MonthSecs  int
	TotalSecs  int
	LastUsed   string // formatted "3 days ago" or empty
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

	now := time.Now()
	rows := make([]projectListRow, 0, len(projects))
	for _, p := range projects {
		acts, _ := s.db.ListActivitiesForProject(r.Context(), p.ID, true)
		row := projectListRow{
			ID: p.ID, Slug: p.Slug, Name: p.Name, Color: p.Color, Archived: p.Archived,
			Activities: len(acts),
		}
		row.TodaySecs = projectSecondsInWindow(r, s, p.ID, now.Add(-24*time.Hour), now)
		row.MonthSecs = projectSecondsInWindow(r, s, p.ID, now.Add(-30*24*time.Hour), now)
		row.TotalSecs = projectSecondsInWindow(r, s, p.ID, time.Unix(0, 0), now)
		row.LastUsed = lastUsedLabel(projects, p, rows)
		_ = lastUsedLabel // keep linter quiet until we wire a real last-used query
		rows = append(rows, row)
	}

	data := projectsPageData{
		Title:        "Projects",
		Active:       "projects",
		Projects:     rows,
		ShowArchived: showArchived,
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash)
	}
	s.renderPageForRequest(w, r, "Projects", "projects", "projects", data)
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
	Flash    string
	FlashOK  bool
}

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

	// Recent sessions in this project (last 50).
	now := time.Now()
	from := now.Add(-30 * 24 * time.Hour)
	rawSessions, err := s.db.ListClosedSessionsInRange(r.Context(), tid, from, now, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views := make([]sessionView, 0, len(rawSessions))
	for _, as := range rawSessions {
		if as.Activity.ProjectID != p.ID {
			continue
		}
		views = append(views, toSessionView(as.Session, as.Activity, from, now, now))
		if len(views) >= 50 {
			break
		}
	}
	hydrateSessionTags(r.Context(), s.db, views)
	hydrateSessionProjects(r.Context(), s.db, views)

	totalSec := 0
	monthSec := 0
	for _, v := range views {
		monthSec += v.AccumulatedSeconds
	}
	data := projectDetailData{
		Title:      p.Name,
		Active:     "projects",
		Project:    p,
		Activities: acts,
		Sessions:   views,
		Total:      fmtDuration(totalSec),
		MonthTotal: fmtDuration(monthSec),
		Archived:   p.Archived,
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash)
	}
	s.renderPageForRequest(w, r, p.Name, "projects", "project-detail", data)
}

// handleProjectNew — GET /projects/new (form page).
func (s *Server) handleProjectNew(w http.ResponseWriter, r *http.Request) {
	data := struct {
		Title  string
		Active string
	}{Title: "New project", Active: "projects"}
	s.renderPageForRequest(w, r, "New project", "projects", "project-new", data)
}

// handleProjectCreateForm — POST /projects/new (form-encoded from the
// create page). Redirects to /projects/{slug} on success.
func (s *Server) handleProjectCreateForm(w http.ResponseWriter, r *http.Request) {
	tid := teamID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	p, err := s.db.CreateProject(r.Context(), tid,
		r.Form.Get("name"), r.Form.Get("slug"), r.Form.Get("color"))
	if err != nil {
		flash := encodeFlash(false, err.Error())
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
	if _, err := s.db.UpdateProject(r.Context(), tid, p.ID, name, color, archived); err != nil {
		flash := encodeFlash(false, err.Error())
		http.Redirect(w, r, "/projects/"+slug+"?flash="+flash, http.StatusSeeOther)
		return
	}
	flash := encodeFlash(true, "updated")
	http.Redirect(w, r, "/projects/"+slug+"?flash="+flash, http.StatusSeeOther)
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

// projectSecondsInWindow sums the duration of closed sessions in a
// project between [from, to]. It issues one IN-list query over the
// project's activities, then sums on the Go side.
func projectSecondsInWindow(r *http.Request, s *Server, projectID int64, from, to time.Time) int {
	ctx := r.Context()
	acts, err := s.db.ListActivitiesForProject(ctx, projectID, true)
	if err != nil || len(acts) == 0 {
		return 0
	}
	ids := make([]int64, len(acts))
	for i, a := range acts {
		ids[i] = a.ID
	}
	rows, err := s.db.SQL().QueryContext(ctx,
		`SELECT activity_id, start_at, end_at, accumulated_seconds, paused
		   FROM sessions
		  WHERE activity_id IN (`+placeholders(len(ids))+`)
		    AND end_at IS NOT NULL
		    AND end_at >= ?
		    AND start_at <= ?`,
		append(toAny(ids), from.UTC().Format("2006-01-02T15:04:05.000"), to.UTC().Format("2006-01-02T15:04:05.000"))...)
	if err != nil {
		return 0
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var aid int64
		var start, endS string
		var accum int
		var paused int
		if err := rows.Scan(&aid, &start, &endS, &accum, &paused); err != nil {
			continue
		}
		st, _ := db.ScanTime(start)
		en, _ := db.ScanTime(endS)
		if en.After(to) {
			en = to
		}
		if st.Before(from) {
			st = from
		}
		if en.After(st) {
			total += int(en.Sub(st).Seconds())
		}
	}
	_ = strconv.Itoa // keep import
	return total
}

// lastUsedLabel is a placeholder; the real implementation lives
// elsewhere if/when we add a "last_used_at" denormalisation. For
// now it returns empty so the template renders a clean "—".
func lastUsedLabel(_ []model.Project, _ model.Project, _ []projectListRow) string {
	return ""
}
