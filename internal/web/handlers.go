package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/auth"
	dbpkg "github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/teams"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// ---------- helpers ------------------------------------------------

// renderPage is the canonical two-step page renderer:
//   1. Execute the page-specific content template into a buffer.
//   2. Wrap the result in the base.html layout, with the buffer as
//      ContentHTML (template.HTML keeps the renderer from re-escaping).
//
// The same data struct is passed to both renders so per-page fields
// like .Period, .Sessions etc. are still in scope when the content
// block runs.
func (s *Server) renderPage(w http.ResponseWriter, title, active, contentTpl string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, contentTpl, data); err != nil {
		http.Error(w, "render content ["+contentTpl+"]: "+err.Error(), http.StatusInternalServerError)
		return
	}
	wrapper := pageData{
		Title:       title,
		Active:      active,
		ContentHTML: template.HTML(buf.String()),
	}
	if err := s.tmpl.ExecuteTemplate(w, "base", wrapper); err != nil {
		http.Error(w, "render base: "+err.Error(), http.StatusInternalServerError)
	}
}

// renderPageForRequest is the auth-aware variant. It pulls the
// authenticated User and current Team out of r.Context() and puts
// them in the wrapper so base.html can render the user menu and the
// current-team switcher. Handlers wrapped by RequireAuth call this.
func (s *Server) renderPageForRequest(w http.ResponseWriter, r *http.Request, title, active, contentTpl string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, contentTpl, data); err != nil {
		http.Error(w, "render content ["+contentTpl+"]: "+err.Error(), http.StatusInternalServerError)
		return
	}
	wrapper := pageData{
		Title:       title,
		Active:      active,
		ContentHTML: template.HTML(buf.String()),
		RequestPath: r.URL.Path,
	}
	var currentUser auth.User
	var currentTeam teams.Team
	if u, ok := UserFrom(r.Context()); ok {
		currentUser = u
		wrapper.User = &currentUser
	}
	if t, ok := TeamFrom(r.Context()); ok {
		currentTeam = t
		wrapper.Team = &currentTeam
		// Workspace switcher list. Cheap — one indexed lookup.
		if list, err := s.teams.ListForUser(r.Context(), currentUser.ID); err == nil {
			for _, tm := range list {
				role, _, _ := s.teams.IsMember(r.Context(), tm.ID, currentUser.ID)
				wrapper.UserTeams = append(wrapper.UserTeams, teamsView{
					ID: tm.ID, Name: tm.Name, Role: string(role),
				})
			}
		}
	}
	if err := s.tmpl.ExecuteTemplate(w, "base", wrapper); err != nil {
		http.Error(w, "render base: "+err.Error(), http.StatusInternalServerError)
	}
}

// renderFragment renders a self-contained template (not wrapped in base).
// Used for HTMX swap targets like active-list and session-row.
func (s *Server) renderFragment(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "render fragment ["+name+"]: "+err.Error(), http.StatusInternalServerError)
	}
}

// teamID returns the current team's id from r.Context(), or 0 if the
// request is unauthenticated (e.g. tests, CLI). All db calls scoped to
// the current workspace pass this as their first argument.
func teamID(r *http.Request) int64 {
	if t, ok := TeamFrom(r.Context()); ok {
		return t.ID
	}
	return 0
}

// render is kept as a thin wrapper for handlers that want the simpler
// signature; it derives title/active from the embedded pageData and
// pulls the authenticated User + Team out of r.Context() so the base
// layout can render the user menu and workspace switcher.
func (s *Server) render(w http.ResponseWriter, r *http.Request, contentTpl string, data any) {
	title := ""
	active := ""
	switch d := data.(type) {
	case dashboardData:
		title, active = d.Title, d.Active
	case statsData:
		title, active = d.Title, d.Active
	case graphData:
		title, active = d.Title, d.Active
	}
	s.renderPageForRequest(w, r, title, active, contentTpl, data)
}

func (s *Server) toast(w http.ResponseWriter, msg, kind string) {
	w.Header().Set("X-Toast", msg)
	if kind != "" {
		w.Header().Set("X-Toast-Kind", kind)
	}
}

func (s *Server) parsePeriod(r *http.Request) timeparse.Period {
	now := time.Now()
	name := r.URL.Query().Get("period")
	if name == "" {
		name = "today"
	}
	if name == "custom" {
		startStr := r.URL.Query().Get("start")
		endStr := r.URL.Query().Get("end")
		if startStr != "" && endStr != "" {
			if start, err := timeparse.ParseDateTime(startStr, now); err == nil {
				if end, err := timeparse.ParseDateTime(endStr, now); err == nil && end.After(start) {
					return timeparse.Period{Start: start, End: end, Label: "custom"}
				}
			}
		}
	}
	p, err := timeparse.ResolvePeriod(name, now)
	if err != nil {
		p, _ = timeparse.ResolvePeriod("today", now)
	}
	return p
}

// ---------- pages --------------------------------------------------

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()
	acts, err := s.db.ListActivities(r.Context(), teamID(r), false)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	active, err := s.db.ListActiveSessions(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	today, _ := timeparse.ResolvePeriod("today", now)
	recent, err := s.db.ListClosedSessionsInRange(r.Context(), teamID(r), today.Start, today.End.Add(24*time.Hour), nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	activeViews := make([]sessionView, 0, len(active))
	for _, as := range active {
		activeViews = append(activeViews, toSessionView(as.Session, as.Activity, today.Start, today.End, now))
	}
	recentViews := make([]sessionView, 0, len(recent))
	for _, as := range recent {
		recentViews = append(recentViews, toSessionView(as.Session, as.Activity, today.Start, today.End, now))
	}
	hydrateSessionTags(ctx, s.db, activeViews)
	hydrateSessionProjects(ctx, s.db, activeViews)
	hydrateSessionTags(ctx, s.db, recentViews)
	hydrateSessionProjects(ctx, s.db, recentViews)
	if len(recentViews) > 8 {
		recentViews = recentViews[:8]
	}

	d := dashboardData{
		pageData:       pageData{Title: "Dashboard", Active: "dashboard"},
		Activities:     acts,
		ActiveSessions: activeViews,
		Recent:         recentViews,
		ActiveCount:    len(activeViews),
	}
	if projects, err := s.db.ListProjects(r.Context(), teamID(r), false); err == nil {
		d.Projects = projects
	}
	// Quick today stats: total tracked time, top activity.
	agg := map[string]int{}
	total := 0
	for _, sv := range recentViews {
		secs := parseHMSStrict(sv.Duration)
		agg[sv.ActivityName] += secs
		total += secs
	}
	d.TodayTotal = fmtDuration(total)
	var topName string
	topSec := 0
	for n, s := range agg {
		if s > topSec {
			topName = n
			topSec = s
		}
	}
	d.TopToday = shortSummary(topName)

	// Goal progress for the dashboard widget. Best-effort: if the goals
	// query fails we just hide the widget by passing an empty slice.
	if progress, err := s.db.ProgressForGoals(r.Context(), teamID(r), now); err == nil {
		d.Goals = toGoalViews(progress)
	} else {
		d.Goals = nil
	}
	s.render(w, r, "dashboard-content", d)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()
	period := s.parsePeriod(r)

	// Optional project filter (?project=slug). Applied at the SQL layer
	// via ListClosedSessionsInRange so we don't pull a hundred rows to
	// drop ninety of them.
	projectFilter := strings.TrimSpace(r.URL.Query().Get("project"))
	var filterProjectID int64
	if projectFilter != "" {
		if proj, err := s.db.GetProjectBySlug(ctx, teamID(r), projectFilter); err == nil {
			filterProjectID = proj.ID
		} else {
			projectFilter = ""
		}
	}

	// Pull all projects for the chip-row + name/color lookups below.
	projects, _ := s.db.ListProjects(ctx, teamID(r), false)
	projByID := map[int64]model.Project{}
	for _, p := range projects {
		projByID[p.ID] = p
	}

	sessions, err := s.db.ListClosedSessionsInRange(ctx, teamID(r), period.Start, period.End, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	agg := map[string]int{}
	projAgg := map[int64]int{} // project_id → total seconds
	total := 0
	rows := make([]sessionView, 0, len(sessions))
	for _, as := range sessions {
		if filterProjectID > 0 && as.Activity.ProjectID != filterProjectID {
			continue
		}
		clipped := clipSeconds(as.Session, period.Start, period.End)
		if clipped <= 0 {
			continue
		}
		agg[as.Activity.Name] += clipped
		projAgg[as.Activity.ProjectID] += clipped
		total += clipped
		rows = append(rows, toSessionView(as.Session, as.Activity, period.Start, period.End, now))
	}

	var aggs []aggRow
	for name, sec := range agg {
		share := 0.0
		if total > 0 {
			share = float64(sec) / float64(total) * 100
		}
		aggs = append(aggs, aggRow{
			ActivityName: name,
			Color:        colorFor(name),
			Duration:     fmtDuration(sec),
			Share:        share,
		})
	}
	sortAggsDesc(aggs)

	// Build the project-grouped breakdown: project totals, plus the
	// activity breakdown nested inside each project. Sorted by total
	// descending so the biggest project is on top.
	type pidKey = int64
	byActivityInProject := map[pidKey]map[string]int{}
	for _, as := range sessions {
		if filterProjectID > 0 && as.Activity.ProjectID != filterProjectID {
			continue
		}
		clipped := clipSeconds(as.Session, period.Start, period.End)
		if clipped <= 0 {
			continue
		}
		m := byActivityInProject[as.Activity.ProjectID]
		if m == nil {
			m = map[string]int{}
			byActivityInProject[as.Activity.ProjectID] = m
		}
		m[as.Activity.Name] += clipped
	}
	byProject := make([]projectAggRow, 0, len(projAgg))
	for pid, sec := range projAgg {
		row := projectAggRow{
			ProjectID: pid,
			Duration:  fmtDuration(sec),
			Share:     0,
		}
		if total > 0 {
			row.Share = float64(sec) / float64(total) * 100
		}
		if p, ok := projByID[pid]; ok {
			row.ProjectName = p.Name
			row.Slug = p.Slug
			row.Color = p.Color
		} else {
			row.ProjectName = "Uncategorized"
			row.Color = "#9ca3af"
		}
		// activities nested
		m := byActivityInProject[pid]
		for name, s := range m {
			share := 0.0
			if sec > 0 {
				share = float64(s) / float64(sec) * 100
			}
			row.Activities = append(row.Activities, aggRow{
				ActivityName: name,
				Color:        colorFor(name),
				Duration:     fmtDuration(s),
				Share:        share,
			})
		}
		sortAggsDesc(row.Activities)
		byProject = append(byProject, row)
	}
	sort.Slice(byProject, func(i, j int) bool {
		return projAgg[byProject[i].ProjectID] > projAgg[byProject[j].ProjectID]
	})

	hydrateSessionTags(ctx, s.db, rows)
	hydrateSessionProjects(ctx, s.db, rows)

	// Tag filter (optional): ?tag=foo. Applied after hydration so the
	// in-memory filter can read each row's Tags slice. Also filters
	// the breakdown so the distribution chart reflects the same set.
	tagFilter := strings.TrimSpace(r.URL.Query().Get("tag"))
	if tagFilter != "" {
		rows, agg, total = filterByTag(rows, agg, total, tagFilter)
		filteredAggs := make([]aggRow, 0, len(aggs))
		for _, a := range aggs {
			if agg[a.ActivityName] > 0 {
				filteredAggs = append(filteredAggs, a)
			}
		}
		aggs = filteredAggs
	}

	allTags, _ := s.db.ListTags(r.Context(), teamID(r))
	allTagNames := make([]string, len(allTags))
	for i, t := range allTags {
		allTagNames[i] = t.Name
	}

	s.render(w, r, "stats-content", statsData{
		pageData:      pageData{Title: "Stats", Active: "stats"},
		Period:        period,
		Aggregated:    aggs,
		ByProject:     byProject,
		Projects:      projects,
		ProjectFilter: projectFilter,
		Sessions:      rows,
		Total:         fmtDuration(total),
		SessionCount:  len(rows),
		TagFilter:     tagFilter,
		AllTagNames:   allTagNames,
	})
}

// filterByTag reduces the rows + aggregate maps to only those carrying
// the named tag. Since we already loaded everything from the DB the
// filtering is in-memory — fine for thousands of rows, but if the
// count grows past tens of thousands a SQL-side join would be the
// right move.
func filterByTag(rows []sessionView, agg map[string]int, total int, tagName string) ([]sessionView, map[string]int, int) {
	filtered := make([]sessionView, 0, len(rows))
	newAgg := map[string]int{}
	newTotal := 0
	for _, r := range rows {
		has := false
		for _, t := range r.Tags {
			if t.Name == tagName {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		filtered = append(filtered, r)
		secs := parseHMSStrict(r.Duration)
		newAgg[r.ActivityName] += secs
		newTotal += secs
	}
	return filtered, newAgg, newTotal
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	period := s.parsePeriod(r)

	sessions, err := s.db.ListClosedSessionsInRange(r.Context(), teamID(r), period.Start, period.End, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	chart := buildChartData(sessions, period)

	chartJSON, _ := json.Marshal(chart)
	s.render(w, r, "graph-content", graphData{
		pageData: pageData{Title: "Graph", Active: "graph"},
		Period:   period,
		Chart:    chart,
		ChartJSON: string(chartJSON),
	})
}

// ---------- JSON / fragments ---------------------------------------

func (s *Server) handleAPIActive(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	today, _ := timeparse.ResolvePeriod("today", now)
	active, err := s.db.ListActiveSessions(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views := make([]sessionView, 0, len(active))
	for _, as := range active {
		views = append(views, toSessionView(as.Session, as.Activity, today.Start, today.End, now))
	}
	hydrateSessionTags(r.Context(), s.db, views)
	hydrateSessionProjects(r.Context(), s.db, views)
	s.renderFragment(w, "active-list", views)
}

// ---------- POST /api/start ---------------------------------------

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("activity"))
	if name == "" {
		http.Error(w, "activity is required", 400)
		return
	}
	note := strings.TrimSpace(r.FormValue("note"))
	act, err := s.db.GetOrCreateActivity(r.Context(), teamID(r), name)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Optional: bind the activity to a project on first start.
	if pidStr := r.FormValue("project_id"); pidStr != "" {
		if pid, err := strconv.ParseInt(pidStr, 10, 64); err == nil && pid > 0 {
			if err := s.db.AssignActivityProject(r.Context(), teamID(r), act.ID, pid); err != nil {
				// non-fatal: log via the http error response but keep going
				// so the user doesn't lose their session start.
				_ = err
			}
			// Refresh the local copy so the rest of the handler sees the new project.
			if fresh, err := s.db.GetActivity(r.Context(), act.ID); err == nil {
				act = fresh
			}
		}
	}
	// Reject duplicate active session for the same activity.
	active, err := s.db.ListActiveSessions(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	for _, as := range active {
		if as.Session.ActivityID == act.ID {
			http.Error(w, fmt.Sprintf("%q already has an active session", act.Name), 409)
			return
		}
	}
	if _, err := s.db.CreateSession(r.Context(), teamID(r), act.ID, time.Now(), note); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toast(w, "started "+act.Name, "success")
	// HTMX target was the active-list fragment — re-render it with the
	// now-complete active list (including the session we just created).
	fresh, err := s.db.ListActiveSessions(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	now := time.Now()
	today, _ := timeparse.ResolvePeriod("today", now)
	views := make([]sessionView, 0, len(fresh))
	for _, as := range fresh {
		views = append(views, toSessionView(as.Session, as.Activity, today.Start, today.End, now))
	}
	hydrateSessionTags(r.Context(), s.db, views)
	hydrateSessionProjects(r.Context(), s.db, views)
	s.renderFragment(w, "active-list", views)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx := r.Context()
	sess, err := s.db.GetSession(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if sess.EndAt != nil {
		http.Error(w, "already stopped", 400)
		return
	}
	if _, err := s.db.UpdateSessionEnd(ctx, id, time.Now()); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toast(w, "stopped session", "success")
	s.respondActiveList(w, r)
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx := r.Context()
	sess, err := s.db.GetSession(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if sess.Paused {
		http.Error(w, "already paused", 400)
		return
	}
	if sess.EndAt != nil {
		http.Error(w, "session already stopped", 400)
		return
	}
	if _, err := s.db.PauseSession(ctx, id, time.Now()); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toast(w, "paused", "success")
	s.respondActiveList(w, r)
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx := r.Context()
	sess, err := s.db.GetSession(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if !sess.Paused {
		http.Error(w, "not paused", 400)
		return
	}
	if _, err := s.db.ResumeSession(ctx, id, time.Now()); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toast(w, "resumed", "success")
	s.respondActiveList(w, r)
}

func (s *Server) handleFocus(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "name is required", 400)
		return
	}
	ctx := r.Context()
	act, err := s.db.FindActivityByName(r.Context(), teamID(r), name)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	active, err := s.db.ListActiveSessions(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	now := time.Now()
	targetExists := false
	for _, as := range active {
		if as.Activity.ID == act.ID {
			targetExists = true
			if as.Session.Paused {
				_, _ = s.db.ResumeSession(ctx, as.Session.ID, now)
			}
			continue
		}
		if !as.Session.Paused {
			_, _ = s.db.PauseSession(ctx, as.Session.ID, now)
		}
	}
	if !targetExists {
		_, _ = s.db.CreateSession(r.Context(), teamID(r), act.ID, now, "")
	}
	s.toast(w, "focused on "+act.Name, "success")
	s.respondActiveList(w, r)
}

func (s *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	startStr := r.FormValue("start_at")
	endStr := r.FormValue("end_at")
	durationStr := r.FormValue("duration")
	note := r.FormValue("note")
	ctx := r.Context()

	// Whichever field is non-empty is updated.
	updates := []any{}
	sets := []string{}
	var startTime time.Time
	hasStart := false
	if startStr != "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", startStr, time.Local)
		if err != nil {
			http.Error(w, "bad start: "+err.Error(), 400)
			return
		}
		startTime = t
		hasStart = true
		sets = append(sets, "start_at = ?")
		updates = append(updates, t.UTC().Format(time.RFC3339Nano))
	}
	if endStr != "" {
		t, err := time.ParseInLocation("2006-01-02T15:04", endStr, time.Local)
		if err != nil {
			http.Error(w, "bad end: "+err.Error(), 400)
			return
		}
		sets = append(sets, "end_at = ?")
		updates = append(updates, t.UTC().Format(time.RFC3339Nano))
	}
	// Duration takes priority: it overrides end_at by computing
	// end = start + duration. If no start given, fetch current.
	if durationStr != "" {
		secs, err := timeparse.ParseDuration(durationStr)
		if err != nil {
			http.Error(w, "bad duration: "+err.Error(), 400)
			return
		}
		if !hasStart {
			// Need current start to anchor the new end.
			sess, err := s.db.GetSession(ctx, id)
			if err != nil {
				http.Error(w, err.Error(), 404)
				return
			}
			startTime = sess.StartAt
		}
		newEnd := startTime.Add(time.Duration(secs) * time.Second)
		sets = append(sets, "end_at = ?")
		updates = append(updates, newEnd.UTC().Format(time.RFC3339Nano))
	}
	// Always allow note updates.
	sets = append(sets, "note = ?")
	updates = append(updates, nullableStr(note))
	sets = append(sets, "updated_at = ?")
	updates = append(updates, time.Now().UTC().Format(time.RFC3339Nano))
	updates = append(updates, id)
	q := "UPDATE sessions SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	if _, err := s.db.SQL().ExecContext(ctx, q, updates...); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Re-render the single updated row.
	sess, err := s.db.GetSession(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	act, err := s.db.GetActivity(ctx, sess.ActivityID)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	now := time.Now()
	period := s.parsePeriod(r)
	view := toSessionView(sess, act, period.Start, period.End, now)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.toast(w, "saved", "success")
	if err := s.tmpl.ExecuteTemplate(w, "session-row", view); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.db.DeleteSession(r.Context(), id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toast(w, "deleted", "success")
	w.WriteHeader(200)
}

// ---------- Tags ---------------------------------------------------

// handleTagsList returns every tag as JSON.
func (s *Server) handleTagsList(w http.ResponseWriter, r *http.Request) {
	tags, err := s.db.ListTags(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if tags == nil {
		tags = []model.Tag{}
	}
	writeJSON(w, map[string]any{"tags": tags})
}

// handleTagsCreate adds a tag (or returns the existing one if the
// name is already taken). JSON body: {"name": "..."}.
func (s *Server) handleTagsCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", 400)
		return
	}
	t, err := s.db.CreateTag(r.Context(), teamID(r), name)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.toast(w, fmt.Sprintf("tag %q ready", t.Name), "success")
	writeJSON(w, t)
}

// handleTagsDelete removes a tag by id. Query: ?id=N.
func (s *Server) handleTagsDelete(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimSpace(r.URL.Query().Get("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "id query param required (int64)", 400)
		return
	}
	if err := s.db.DeleteTag(r.Context(), id); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toast(w, "tag deleted", "success")
	w.WriteHeader(200)
}

// handleSessionTagAdd attaches a tag (auto-created if new) to a session.
// Body: name=...
func (s *Server) handleSessionTagAdd(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		http.Error(w, "name is required", 400)
		return
	}
	if err := s.db.AttachTag(r.Context(), teamID(r), id, name); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toast(w, fmt.Sprintf("tagged %s", name), "success")
	w.WriteHeader(200)
}

// handleSessionTagRemove detaches a tag from a session. Query: ?name=...
func (s *Server) handleSessionTagRemove(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Error(w, "name query param required", 400)
		return
	}
	if err := s.db.DetachTag(r.Context(), teamID(r), id, name); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toast(w, fmt.Sprintf("untagged %s", name), "success")
	w.WriteHeader(200)
}

// handleTagsPage serves /tags.
func (s *Server) handleTagsPage(w http.ResponseWriter, r *http.Request) {
	tags, err := s.db.ListAllTagsWithCounts(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views := make([]tagWithCount, 0, len(tags))
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		views = append(views, tagWithCount{
			tagChip:      tagChip{ID: t.ID, Name: t.Name},
			SessionCount: t.SessionCount,
		})
		names = append(names, t.Name)
	}
	s.render(w, r, "tags-content", tagsData{
		pageData:    pageData{Title: "Tags", Active: "tags"},
		Tags:        views,
		AllTagNames: names,
	})
}

// handleTagsFragment returns the inner `tags-list` template so HTMX
// can swap it without a full page reload.
func (s *Server) handleTagsFragment(w http.ResponseWriter, r *http.Request) {
	tags, err := s.db.ListAllTagsWithCounts(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views := make([]tagWithCount, 0, len(tags))
	for _, t := range tags {
		views = append(views, tagWithCount{
			tagChip:      tagChip{ID: t.ID, Name: t.Name},
			SessionCount: t.SessionCount,
		})
	}
	s.renderFragment(w, "tags-list", views)
}

// ---------- Goals ---------------------------------------------------

// handleGoals serves the /goals management page.
func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	acts, err := s.db.ListActivities(r.Context(), teamID(r), false)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	progress, err := s.db.ProgressForGoals(r.Context(), teamID(r), now)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	data := struct {
		pageData
		Activities []model.Activity
		Goals      []goalView
	}{
		pageData:   pageData{Title: "Goals", Active: "goals"},
		Activities: acts,
		Goals:      toGoalViews(progress),
	}
	s.render(w, r, "goals-content", data)
}

// handleGoalsList returns all configured goals as JSON (no progress).
func (s *Server) handleGoalsList(w http.ResponseWriter, r *http.Request) {
	goals, err := s.db.ListGoals(r.Context(), teamID(r), nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if goals == nil {
		goals = []model.Goal{}
	}
	writeJSON(w, map[string]any{"goals": goals})
}

// handleGoalsProgress returns goal + current-period progress for each
// configured goal. Powers the dashboard widget.
//
// When the request comes from HTMX (HX-Request header), the response is
// the rendered `goals-list` fragment so it can be swapped into the
// page directly. Plain GET returns JSON for tooling / scripts.
func (s *Server) handleGoalsProgress(w http.ResponseWriter, r *http.Request) {
	progress, err := s.db.ProgressForGoals(r.Context(), teamID(r), time.Now())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views := toGoalViews(progress)
	if r.Header.Get("HX-Request") == "true" {
		s.renderFragment(w, "goals-list", views)
		return
	}
	if progress == nil {
		progress = []dbpkg.GoalProgress{}
	}
	writeJSON(w, map[string]any{"progress": progress})
}

// handleGoalsUpsert creates or replaces a goal. Body params:
//   activity   (required)
//   period     required — daily | weekly | monthly
//   minutes    required — integer target in minutes
func (s *Server) handleGoalsUpsert(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	activityName := strings.TrimSpace(r.FormValue("activity"))
	period := strings.TrimSpace(r.FormValue("period"))
	minsStr := strings.TrimSpace(r.FormValue("minutes"))
	if activityName == "" || period == "" || minsStr == "" {
		http.Error(w, "activity, period and minutes are required", 400)
		return
	}
	mins, err := strconv.Atoi(minsStr)
	if err != nil || mins <= 0 {
		http.Error(w, "minutes must be a positive integer", 400)
		return
	}
	act, err := s.db.GetOrCreateActivity(r.Context(), teamID(r), activityName)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	g, err := s.db.UpsertGoal(r.Context(), teamID(r), act.ID, period, mins)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.toast(w, fmt.Sprintf("set %s goal: %dm/%s", act.Name, g.TargetMinutes, g.Period), "success")
	writeJSON(w, g)
}

// handleGoalsDelete removes a goal. Query params: activity + period.
func (s *Server) handleGoalsDelete(w http.ResponseWriter, r *http.Request) {
	activityName := strings.TrimSpace(r.URL.Query().Get("activity"))
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if activityName == "" || period == "" {
		http.Error(w, "activity and period query params are required", 400)
		return
	}
	act, err := s.db.GetActivityByName(r.Context(), teamID(r), activityName)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.db.DeleteGoal(r.Context(), teamID(r), act.ID, period); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toast(w, fmt.Sprintf("removed %s goal for %s", period, activityName), "success")
	w.WriteHeader(200)
}

// writeJSON is a tiny helper used by goal endpoints; keeps the handlers
// short and avoids importing encoding/json at the top of the file.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// toGoalViews renders []db.GoalProgress as the view-models used by
// dashboard and /goals pages.
func toGoalViews(progress []dbpkg.GoalProgress) []goalView {
	out := make([]goalView, 0, len(progress))
	for _, p := range progress {
		class := ""
		switch {
		case p.PercentComplete >= 100:
			class = "exceeded"
		case p.PercentComplete >= 80:
			class = "met"
		}
		out = append(out, goalView{
			ID:               p.Goal.ID,
			ActivityName:     p.ActivityName,
			Color:            colorFor(p.ActivityName),
			Period:           p.Goal.Period,
			TargetMinutes:    p.Goal.TargetMinutes,
			TargetLabel:      formatMinutes(p.Goal.TargetMinutes),
			AchievedMinutes:  p.AchievedMinutes,
			AchievedLabel:    formatMinutes(p.AchievedMinutes),
			Percent:          p.PercentComplete,
			AchievedClass:    class,
			PeriodStartLabel: p.PeriodStart.Local().Format("Jan 2"),
			PeriodEndLabel:   p.PeriodEnd.Local().Format("Jan 2"),
			PeriodRangeLabel: periodRangeLabel(p.Goal.Period),
		})
	}
	return out
}

// formatMinutes renders an integer minute count as a short label.
func formatMinutes(min int) string {
	if min < 60 {
		return fmt.Sprintf("%dm", min)
	}
	h := min / 60
	m := min % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// periodRangeLabel returns a short human label for the goal period.
func periodRangeLabel(period string) string {
	switch period {
	case "daily":
		return "today"
	case "weekly":
		return "this week"
	case "monthly":
		return "this month"
	}
	return period
}

func (s *Server) handleCSV(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="paratrack.csv"`)
	sessions, err := s.db.ListClosedSessionsInRange(r.Context(), teamID(r),
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), time.Now().Add(24*time.Hour), nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Look up project names in one IN-list query so the per-row join
	// is O(1) instead of one extra round-trip per session.
	projNameByID := map[int64]string{}
	if len(sessions) > 0 {
		seen := map[int64]struct{}{}
		var pids []int64
		for _, as := range sessions {
			if as.Activity.ProjectID == 0 {
				continue
			}
			if _, ok := seen[as.Activity.ProjectID]; ok {
				continue
			}
			seen[as.Activity.ProjectID] = struct{}{}
			pids = append(pids, as.Activity.ProjectID)
		}
		if len(pids) > 0 {
			rows, _ := s.db.SQL().QueryContext(r.Context(),
				`SELECT id, slug FROM projects WHERE id IN (`+placeholders(len(pids))+`)`, toAny(pids)...)
			if rows != nil {
				for rows.Next() {
					var id int64
					var slug string
					if err := rows.Scan(&id, &slug); err == nil {
						projNameByID[id] = slug
					}
				}
				rows.Close()
			}
		}
	}
	fmt.Fprintln(w, "id,activity,project,start,end,duration_seconds,note")
	for _, as := range sessions {
		end := time.Time{}
		if as.Session.EndAt != nil {
			end = *as.Session.EndAt
		}
		dur := ""
		if as.Session.EndAt != nil {
			dur = strconv.Itoa(int(as.Session.EndAt.Sub(as.Session.StartAt).Seconds()))
		}
		note := ""
		if as.Session.Note != nil {
			note = *as.Session.Note
		}
		project := "" // "" = Uncategorized in the CSV
		if slug, ok := projNameByID[as.Activity.ProjectID]; ok {
			project = slug
		}
		fmt.Fprintf(w, "%d,%q,%q,%s,%s,%s,%q\n",
			as.Session.ID, as.Activity.Name, project,
			as.Session.StartAt.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339),
			dur, note)
	}
}

// ---------- shared helpers ----------------------------------------

// respondActiveList is the standard "refresh the active sessions list"
// response sent by stop/pause/resume/focus. Keeps HTMX swap targets
// consistent across actions.
func (s *Server) respondActiveList(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	today, _ := timeparse.ResolvePeriod("today", now)
	active, err := s.db.ListActiveSessions(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views := make([]sessionView, 0, len(active))
	for _, as := range active {
		views = append(views, toSessionView(as.Session, as.Activity, today.Start, today.End, now))
	}
	hydrateSessionTags(r.Context(), s.db, views)
	hydrateSessionProjects(r.Context(), s.db, views)
	s.renderFragment(w, "active-list", views)
}

func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

func parseHMSStrict(hms string) int {
	parts := strings.Split(hms, ":")
	if len(parts) != 3 {
		return 0
	}
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	sec, _ := strconv.Atoi(parts[2])
	return h*3600 + m*60 + sec
}

// nullableStr returns nil for empty string so the SQL driver binds NULL.
func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// sortAggsDesc sorts aggregated rows by share (desc) using a simple
// in-place sort so we don't pull in sort.Slice boilerplate per handler.
func sortAggsDesc(rows []aggRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Share > rows[j-1].Share; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

func clipSeconds(sess model.Session, start, end time.Time) int {
	if sess.EndAt == nil {
		return 0
	}
	se := sess.StartAt
	ee := *sess.EndAt
	if se.Before(start) {
		se = start
	}
	if ee.After(end) {
		ee = end
	}
	if !ee.After(se) {
		return 0
	}
	// Round up sub-second overlaps to 1. Without this, a session that
	// literally just started (or one whose end was clamped to "now")
	// can have an overlap of ~0.1s, which truncates to 0 seconds and
	// disappears from /stats entirely.
	return int(math.Ceil(ee.Sub(se).Seconds()))
}
