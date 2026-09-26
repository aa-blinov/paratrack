package web

import (
	"net/url"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/i18n"
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
// stampLang sets Lang on standalone auth-page structs (login, register,
// forgot, reset) that expose the field but do not implement langCarrier.
func stampLang(data any, lang i18n.Lang) {
	type langField interface{ setLang(string) }
	if l, ok := data.(langField); ok {
		l.setLang(string(lang))
		return
	}
	// Fallback for value structs with a Lang string field we can address
	// through their pointer form — callers pass &data.
}

// renderPage renders a public (pre-auth) page. It still mints a CSRF
// cookie so the login / register / reset forms can submit safely.
// Callers that use standalone structs should set their own CSRFToken
// field via ensureCSRF before calling.
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, title, active, contentTpl string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	token := ensureCSRF(w, r)
	lang := resolveLang(r)
	if c, ok := data.(csrfCarrier); ok {
		c.setCSRF(token)
	}
	if l, ok := data.(langCarrier); ok {
		l.setLang(string(lang))
	}
	stampLang(data, lang)
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, contentTpl, data); err != nil {
		http.Error(w, "render content ["+contentTpl+"]: "+err.Error(), http.StatusInternalServerError)
		return
	}
	wrapper := pageData{
		Title:       pageTitle(lang, title),
		Active:      active,
		ContentHTML: template.HTML(buf.String()),
		CSRFToken:   token,
		Lang:        string(lang),
		RequestPath: r.URL.Path,
	}
	if err := s.tmpl.ExecuteTemplate(w, "base", wrapper); err != nil {
		http.Error(w, "render base: "+err.Error(), http.StatusInternalServerError)
	}
}

// actionTime is when a timer action happened: the client's click time
// for actions replayed from the offline queue ("client_ts", unix ms),
// otherwise now. Only the last 24h is trusted.
func actionTime(r *http.Request) time.Time {
	now := time.Now()
	ms, err := strconv.ParseInt(r.FormValue("client_ts"), 10, 64)
	if err != nil {
		return now
	}
	t := time.UnixMilli(ms)
	if t.After(now) || now.Sub(t) > 24*time.Hour {
		return now
	}
	return t
}

// pageTitle translates a handler's English page title ("Dashboard")
// via "title.<Title>"; dynamic titles (a project name) pass through.
func pageTitle(lang i18n.Lang, title string) string {
	key := "title." + title
	if t := i18n.T(lang, key); t != key {
		return t
	}
	return title
}

// renderPageForRequest is the auth-aware variant. It pulls the
// authenticated User and current Team out of r.Context() and puts
// them in the wrapper so base.html can render the user menu and the
// current-team switcher. Handlers wrapped by RequireAuth call this.
func (s *Server) renderPageForRequest(w http.ResponseWriter, r *http.Request, title, active, contentTpl string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	token := ensureCSRF(w, r)
	lang := resolveLang(r)
	if c, ok := data.(csrfCarrier); ok {
		c.setCSRF(token)
	}
	if l, ok := data.(langCarrier); ok {
		l.setLang(string(lang))
	}
	stampLang(data, lang)
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, contentTpl, data); err != nil {
		http.Error(w, "render content ["+contentTpl+"]: "+err.Error(), http.StatusInternalServerError)
		return
	}
	wrapper := pageData{
		Title:       pageTitle(lang, title),
		Active:      active,
		ContentHTML: template.HTML(buf.String()),
		RequestPath: r.URL.Path,
		CSRFToken:   token,
		Lang:        string(lang),
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
	if name == "active-list" {
		// Every timer action lands here; the "Today" card listens and refreshes.
		w.Header().Set("HX-Trigger", "sessions-changed")
	}
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
// signature; it derives title/active from the view-model and pulls the
// authenticated User + Team out of r.Context() so the base layout can
// render the user menu and workspace switcher.
func (s *Server) render(w http.ResponseWriter, r *http.Request, contentTpl string, data any) {
	title := ""
	active := ""
	if pm, ok := data.(pageMeta); ok {
		title, active = pm.pageInfo()
	}
	s.renderPageForRequest(w, r, title, active, contentTpl, data)
}

// toastL is toast() with a dictionary key + optional detail, resolved
// in the request language.
func (s *Server) toastL(w http.ResponseWriter, r *http.Request, key, detail, kind string) {
	msg := i18n.T(resolveLang(r), key)
	if detail != "" {
		msg += " " + detail
	}
	s.toast(w, msg, kind)
}

func (s *Server) toast(w http.ResponseWriter, msg, kind string) {
	// Header values are Latin-1 on the wire: percent-encode so Cyrillic
	// survives, the client decodes with decodeURIComponent.
	w.Header().Set("X-Toast", url.PathEscape(msg))
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
	// Recent window matches the card label: last 7 days (plus a day of
	// slack so a session that ended after midnight still shows).
	weekFrom := now.Add(-7 * 24 * time.Hour)
	weekTo := now.Add(24 * time.Hour)
	todaySessions, err := s.db.ListClosedSessionsInRange(ctx, teamID(r), today.Start, today.End.Add(24*time.Hour), nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	weekSessions, err := s.db.ListClosedSessionsInRange(ctx, teamID(r), weekFrom, weekTo, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	activeViews := make([]sessionView, 0, len(active))
	for _, as := range active {
		activeViews = append(activeViews, toSessionView(as.Session, as.Activity, today.Start, today.End, now, resolveLang(r)))
	}
	todayViews := make([]sessionView, 0, len(todaySessions))
	for _, as := range todaySessions {
		todayViews = append(todayViews, toSessionView(as.Session, as.Activity, today.Start, today.End, now, resolveLang(r)))
	}
	recentViews := make([]sessionView, 0, len(weekSessions))
	for _, as := range weekSessions {
		recentViews = append(recentViews, toSessionView(as.Session, as.Activity, weekFrom, weekTo, now, resolveLang(r)))
	}
	hydrateSessionTags(ctx, s.db, activeViews)
	hydrateSessionProjects(ctx, s.db, activeViews)
	hydrateSessionTags(ctx, s.db, todayViews)
	hydrateSessionProjects(ctx, s.db, todayViews)
	hydrateSessionTags(ctx, s.db, recentViews)
	hydrateSessionProjects(ctx, s.db, recentViews)
	if len(recentViews) > 8 {
		recentViews = recentViews[:8]
	}

	lang := string(resolveLang(r))
	d := dashboardData{
		pageData:       pageData{Title: "Dashboard", Active: "dashboard", Lang: lang},
		Activities:     acts,
		ActiveSessions: activeViews,
		Recent:         recentViews,
		ActiveCount:    len(activeViews),
		ActiveVM:       activeListVM{Lang: lang, Items: activeViews},
	}
	if projects, err := s.db.ListProjects(r.Context(), teamID(r), false); err == nil {
		d.Projects = projects
		d.HasProject = len(projects) > 0
	}
	// First-run checklist: the account is new until it has any session at all.
	d.HasSession = len(activeViews) > 0 || len(recentViews) > 0
	d.ShowOnboard = !d.HasSession
	// Quick today stats: total tracked time, top activity. Aggregates
	// read DurationSecs — never parse the human label.
	agg := map[string]int{}
	total := 0
	// Running timers count too, so "tracked today" moves while you work.
	for _, sv := range append(todayViews, activeViews...) {
		agg[sv.ActivityName] += sv.DurationSecs
		total += sv.DurationSecs
	}
	d.TodayTotal = fmtDur(r, total)
	var topName string
	topSec := 0
	for n, sec := range agg {
		if sec > topSec {
			topName = n
			topSec = sec
		}
	}
	d.TopToday = shortSummary(topName)

	// Goal progress for the dashboard widget. Best-effort: if the goals
	// query fails we just hide the widget by passing an empty slice.
	if progress, err := s.db.ProgressForGoals(r.Context(), teamID(r), now); err == nil {
		d.Goals = toGoalViews(progress, resolveLang(r))
		for i := range d.Goals {
			d.Goals[i].Lang = lang
			d.Goals[i].PeriodRangeLabel = periodRangeLabel(d.Goals[i].Period, i18n.Lang(lang))
		}
	} else {
		d.Goals = nil
	}
	d.GoalsVM = goalsListVM{Lang: lang, Goals: d.Goals}
	s.render(w, r, "dashboard-content", &d)
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
	rows := make([]sessionView, 0, len(sessions))
	for _, as := range sessions {
		if filterProjectID > 0 && as.Activity.ProjectID != filterProjectID {
			continue
		}
		clipped := clipSeconds(as.Session, period.Start, period.End)
		if clipped <= 0 {
			continue
		}
		rows = append(rows, toSessionView(as.Session, as.Activity, period.Start, period.End, now, resolveLang(r)))
	}
	hydrateSessionTags(ctx, s.db, rows)
	hydrateSessionProjects(ctx, s.db, rows)

	// Tag filter (?tag=foo) runs BEFORE aggregation so the breakdown,
	// distribution bar and totals all describe the same row set.
	tagFilter := strings.TrimSpace(r.URL.Query().Get("tag"))
	if tagFilter != "" {
		rows = filterByTag(rows, tagFilter)
	}

	// Aggregates read DurationSecs — never parse the human label.
	agg := map[string]int{}
	projAgg := map[int64]int{} // project_id → total seconds
	total := 0
	for _, sv := range rows {
		agg[sv.ActivityName] += sv.DurationSecs
		projAgg[sv.ProjectID] += sv.DurationSecs
		total += sv.DurationSecs
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
			Duration:     fmtDur(r, sec),
			Share:        share,
		})
	}
	sortAggsDesc(aggs)

	// Build the project-grouped breakdown: project totals, plus the
	// activity breakdown nested inside each project. Sorted by total
	// descending so the biggest project is on top.
	byActivityInProject := map[int64]map[string]int{}
	for _, sv := range rows {
		m := byActivityInProject[sv.ProjectID]
		if m == nil {
			m = map[string]int{}
			byActivityInProject[sv.ProjectID] = m
		}
		m[sv.ActivityName] += sv.DurationSecs
	}
	byProject := make([]projectAggRow, 0, len(projAgg))
	for pid, sec := range projAgg {
		row := projectAggRow{
			ProjectID: pid,
			Duration:  fmtDur(r, sec),
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
			row.ProjectName = i18n.T(resolveLang(r), "dash.uncategorized")
			row.Color = "#9ca3af"
		}
		// activities nested
		m := byActivityInProject[pid]
		for name, actSec := range m {
			// Same base as the project row, so the "Share" column adds up.
			share := 0.0
			if total > 0 {
				share = float64(actSec) / float64(total) * 100
			}
			row.Activities = append(row.Activities, aggRow{
				ActivityName: name,
				Color:        colorFor(name),
				Duration:     fmtDur(r, actSec),
				Share:        share,
			})
		}
		sortAggsDesc(row.Activities)
		byProject = append(byProject, row)
	}
	sort.Slice(byProject, func(i, j int) bool {
		return projAgg[byProject[i].ProjectID] > projAgg[byProject[j].ProjectID]
	})

	allTags, _ := s.db.ListTags(r.Context(), teamID(r))
	allTagNames := make([]string, len(allTags))
	for i, t := range allTags {
		allTagNames[i] = t.Name
	}

	s.render(w, r, "stats-content", &statsData{
		pageData:      pageData{Title: "Stats", Active: "stats"},
		Period:        period,
		Aggregated:    aggs,
		ByProject:     byProject,
		Projects:      projects,
		ProjectFilter: projectFilter,
		Sessions:      rows,
		Total:         fmtDur(r, total),
		SessionCount:  len(rows),
		TagFilter:     tagFilter,
		AllTagNames:   allTagNames,
		SavedReports:  s.loadSavedReports(r),
	})
}

// filterByTag reduces the rows to only those carrying the named tag.
// Since we already loaded everything from the DB the filtering is
// in-memory — fine for thousands of rows, but if the count grows past
// tens of thousands a SQL-side join would be the right move.
func filterByTag(rows []sessionView, tagName string) []sessionView {
	filtered := make([]sessionView, 0, len(rows))
	for _, r := range rows {
		for _, t := range r.Tags {
			if t.Name == tagName {
				filtered = append(filtered, r)
				break
			}
		}
	}
	return filtered
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	period := s.parsePeriod(r)

	sessions, err := s.db.ListClosedSessionsInRange(r.Context(), teamID(r), period.Start, period.End, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	chart := buildChartData(sessions, period, resolveLang(r))

	chartJSON, _ := json.Marshal(chart)
	s.render(w, r, "graph-content", &graphData{
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
		views = append(views, toSessionView(as.Session, as.Activity, today.Start, today.End, now, resolveLang(r)))
	}
	hydrateSessionTags(r.Context(), s.db, views)
	hydrateSessionProjects(r.Context(), s.db, views)
	lang := string(resolveLang(r))
	for i := range views {
		views[i].Lang = lang
	}
	s.renderFragment(w, "active-list", activeListVM{Lang: string(resolveLang(r)), Items: views})
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
				// Surface it — the user explicitly asked for this project.
				http.Error(w, "project: "+err.Error(), 400)
				return
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
	sess, err := s.db.CreateSession(r.Context(), teamID(r), act.ID, actionTime(r), note)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Stamp the owner for payroll attribution.
	if u, ok := UserFrom(r.Context()); ok {
		_, _ = s.db.SQL().ExecContext(r.Context(),
			`UPDATE sessions SET user_id = ? WHERE id = ?`, u.ID, sess.ID)
	}
	s.audit(r, "session.start", strconv.FormatInt(sess.ID, 10), act.Name)
	s.fireWebhook(r, "session.started", map[string]any{"session_id": sess.ID, "activity": act.Name})
	s.toastL(w, r, "toast.started", act.Name, "success")
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
		views = append(views, toSessionView(as.Session, as.Activity, today.Start, today.End, now, resolveLang(r)))
	}
	hydrateSessionTags(r.Context(), s.db, views)
	hydrateSessionProjects(r.Context(), s.db, views)
	lang := string(resolveLang(r))
	for i := range views {
		views[i].Lang = lang
	}
	s.renderFragment(w, "active-list", activeListVM{Lang: string(resolveLang(r)), Items: views})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx := r.Context()
	sess, err := s.db.GetSession(ctx, teamID(r), id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if sess.EndAt != nil {
		http.Error(w, "already stopped", 400)
		return
	}
	stopped, err := s.db.UpdateSessionEnd(ctx, teamID(r), id, actionTime(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.audit(r, "session.stop", strconv.FormatInt(id, 10), "")
	s.fireWebhook(r, "session.stopped", map[string]any{
		"session_id": id, "activity_id": stopped.ActivityID,
		"start": stopped.StartAt.UTC().Format(time.RFC3339),
	})
	pushName := "a session"
	if a, err := s.db.GetActivity(ctx, stopped.ActivityID); err == nil {
		pushName = a.Name
	}
	// Say what stopped, how long it ran, and where it went.
	dur := fmtDur(r, stopped.DurationSeconds(time.Now()))
	if stopped.DurationSeconds(time.Now()) < 60 {
		dur = i18n.T(resolveLang(r), "dur.underMinute")
	}
	s.toast(w, strings.NewReplacer("{name}", pushName, "{dur}", dur).
		Replace(i18n.T(resolveLang(r), "toast.stoppedFull")), "success")
	s.sendPush(teamID(r), "Session stopped", pushName+" finished", "/stats")
	s.notifyNewlyMetGoals(r, stopped.ActivityID)
	s.respondActiveList(w, r)
}

func (s *Server) handlePause(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx := r.Context()
	sess, err := s.db.GetSession(ctx, teamID(r), id)
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
	if _, err := s.db.PauseSession(ctx, teamID(r), id, actionTime(r)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toastL(w, r, "toast.paused", "", "success")
	s.respondActiveList(w, r)
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx := r.Context()
	sess, err := s.db.GetSession(ctx, teamID(r), id)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	if !sess.Paused {
		http.Error(w, "not paused", 400)
		return
	}
	if _, err := s.db.ResumeSession(ctx, teamID(r), id, actionTime(r)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toastL(w, r, "toast.resumed", "", "success")
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
	now := actionTime(r)
	targetExists := false
	for _, as := range active {
		if as.Activity.ID == act.ID {
			targetExists = true
			if as.Session.Paused {
				_, _ = s.db.ResumeSession(ctx, teamID(r), as.Session.ID, now)
			}
			continue
		}
		if !as.Session.Paused {
			_, _ = s.db.PauseSession(ctx, teamID(r), as.Session.ID, now)
		}
	}
	if !targetExists {
		_, _ = s.db.CreateSession(r.Context(), teamID(r), act.ID, now, "")
	}
	s.toastL(w, r, "toast.focused", act.Name, "success")
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
	// A duration wins over end_at (end = start + duration below); setting
	// both would assign end_at twice, which Postgres rejects.
	if endStr != "" && durationStr == "" {
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
			sess, err := s.db.GetSession(ctx, teamID(r), id)
			if err != nil {
				http.Error(w, err.Error(), 404)
				return
			}
			startTime = sess.StartAt
		}
		newEnd := startTime.Add(time.Duration(secs) * time.Second)
		sets = append(sets, "end_at = ?")
		updates = append(updates, newEnd.UTC().Format(time.RFC3339Nano))
		// Hand-editing the interval defines the tracked total too —
		// keep accumulated_seconds in lock-step so DurationSeconds and
		// every aggregate agree with the cell the user just typed.
		sets = append(sets, "accumulated_seconds = ?")
		updates = append(updates, secs)
	} else if endStr != "" {
		// End edited without an explicit duration: tracked = span.
		if !hasStart {
			sess, err := s.db.GetSession(ctx, teamID(r), id)
			if err != nil {
				http.Error(w, err.Error(), 404)
				return
			}
			startTime = sess.StartAt
		}
		// endStr was already parsed into the sets/updates above; re-parse
		// the span here so accumulated_seconds matches start→end.
		endTime, err := time.ParseInLocation("2006-01-02T15:04", endStr, time.Local)
		if err == nil {
			span := int(endTime.Sub(startTime).Seconds())
			if span < 0 {
				span = 0
			}
			sets = append(sets, "accumulated_seconds = ?")
			updates = append(updates, span)
		}
	}
	// Always allow note updates.
	sets = append(sets, "note = ?")
	updates = append(updates, nullableStr(note))
	sets = append(sets, "updated_at = ?")
	updates = append(updates, time.Now().UTC().Format(time.RFC3339Nano))
	updates = append(updates, id)
	q := "UPDATE sessions SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	if teamID(r) > 0 {
		q += " AND team_id = ?"
		updates = append(updates, teamID(r))
	}
	res, err := s.db.SQL().ExecContext(ctx, q, updates...)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if teamID(r) > 0 {
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, "session not found", 404)
			return
		}
	}
	// Re-render the single updated row (with tags + project badge).
	s.toastL(w, r, "toast.saved", "", "success")
	s.respondSessionRow(w, r, id)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if err := s.db.DeleteSession(r.Context(), teamID(r), id); err != nil {
		if errors.Is(err, dbpkg.ErrNotFound) {
			http.Error(w, "session not found", 404)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	s.toastL(w, r, "toast.deleted", "", "success")
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
// HTMX callers get the `tags-list` fragment back so the page list
// refreshes in place; plain requests keep the JSON shape.
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
	s.toastL(w, r, "toast.tagReady", t.Name, "success")
	if isHTMX(r) {
		s.respondTagsList(w, r)
		return
	}
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
	if err := s.db.DeleteTag(r.Context(), teamID(r), id); err != nil {
		if errors.Is(err, dbpkg.ErrTagNotFound) {
			http.Error(w, "tag not found", 404)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	s.toastL(w, r, "toast.tagDeleted", "", "success")
	if isHTMX(r) {
		s.respondTagsList(w, r)
		return
	}
	w.WriteHeader(200)
}

// respondTagsList renders the `tags-list` fragment for HTMX swaps.
func (s *Server) respondTagsList(w http.ResponseWriter, r *http.Request) {
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
			Lang:         string(resolveLang(r)),
		})
	}
	s.renderFragment(w, "tags-list", tagsListVM{Lang: string(resolveLang(r)), Tags: views})
}

// isHTMX reports whether the request came from an HTMX swap target.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

// handleSessionTagAdd attaches a tag (auto-created if new) to a session.
// Body: name=...  HTMX swaps the response into the session row.
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
	s.toastL(w, r, "toast.tagged", name, "success")
	s.respondSessionRow(w, r, id)
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
	s.toastL(w, r, "toast.untagged", name, "success")
	s.respondSessionRow(w, r, id)
}

// respondSessionRow re-renders one stats table row (tags + project
// badge included) so HTMX outerHTML swaps keep the row intact.
func (s *Server) respondSessionRow(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()
	sess, err := s.db.GetSession(ctx, teamID(r), id)
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
	views := []sessionView{toSessionView(sess, act, period.Start, period.End, now, resolveLang(r))}
	hydrateSessionTags(ctx, s.db, views)
	hydrateSessionProjects(ctx, s.db, views)
	views[0].Lang = string(resolveLang(r))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "session-row", views[0]); err != nil {
		http.Error(w, err.Error(), 500)
	}
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
			Lang:         string(resolveLang(r)),
		})
		names = append(names, t.Name)
	}
	s.render(w, r, "tags-content", &tagsData{
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
			Lang:         string(resolveLang(r)),
		})
	}
	s.renderFragment(w, "tags-list", tagsListVM{Lang: string(resolveLang(r)), Tags: views})
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

	gv := toGoalViews(progress, resolveLang(r))
	lang := string(resolveLang(r))
	for i := range gv {
		gv[i].Lang = lang
		gv[i].PeriodRangeLabel = periodRangeLabel(gv[i].Period, i18n.Lang(lang))
	}
	data := struct {
		pageData
		Activities []model.Activity
		Goals      []goalView
		GoalsVM    goalsListVM
	}{
		pageData:   pageData{Title: "Goals", Active: "goals", Lang: lang},
		Activities: acts,
		Goals:      gv,
		GoalsVM:    goalsListVM{Lang: lang, Goals: gv},
	}
	s.render(w, r, "goals-content", &data)
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
	views := toGoalViews(progress, resolveLang(r))
	glang := string(resolveLang(r))
	for i := range views {
		views[i].Lang = glang
		views[i].PeriodRangeLabel = periodRangeLabel(views[i].Period, i18n.Lang(glang))
	}
	if isHTMX(r) {
		s.renderFragment(w, "goals-list", goalsListVM{Lang: glang, Goals: views})
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
//
// HTMX callers get the refreshed `goals-list` fragment; plain requests
// keep the JSON shape.
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
	s.toastL(w, r, "toast.saved", act.Name, "success")
	if isHTMX(r) {
		s.respondGoalsList(w, r)
		return
	}
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
		if errors.Is(err, dbpkg.ErrNotFound) {
			http.Error(w, "activity not found", 404)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.db.DeleteGoal(r.Context(), teamID(r), act.ID, period); err != nil {
		if errors.Is(err, dbpkg.ErrGoalNotFound) {
			http.Error(w, "goal not found", 404)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	s.toastL(w, r, "toast.deleted", activityName, "success")
	if isHTMX(r) {
		s.respondGoalsList(w, r)
		return
	}
	w.WriteHeader(200)
}

// respondGoalsList renders the `goals-list` fragment for HTMX swaps.
func (s *Server) respondGoalsList(w http.ResponseWriter, r *http.Request) {
	progress, err := s.db.ProgressForGoals(r.Context(), teamID(r), time.Now())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	gv := toGoalViews(progress, resolveLang(r))
	lang := string(resolveLang(r))
	for i := range gv {
		gv[i].Lang = lang
		gv[i].PeriodRangeLabel = periodRangeLabel(gv[i].Period, i18n.Lang(lang))
	}
	s.renderFragment(w, "goals-list", goalsListVM{Lang: string(resolveLang(r)), Goals: gv})
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
func toGoalViews(progress []dbpkg.GoalProgress, lang i18n.Lang) []goalView {
	out := make([]goalView, 0, len(progress))
	for _, p := range progress {
		// Lang is stamped by the caller after this returns.
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
			TargetLabel:      fmtDurL(lang, p.Goal.TargetMinutes*60),
			AchievedMinutes:  p.AchievedMinutes,
			AchievedLabel:    fmtDurL(lang, p.AchievedMinutes*60),
			Percent:          p.PercentComplete,
			AchievedClass:    class,
			PeriodStartLabel: fmtDay(lang, p.PeriodStart.Local()),
			PeriodEndLabel:   fmtDay(lang, p.PeriodEnd.Local()),
			PeriodRangeLabel: periodRangeLabel(p.Goal.Period, i18n.En), // caller re-stamps with page lang
		})
	}
	return out
}

// formatMinutes renders an integer minute count as a short label.
// periodRangeLabel returns a short human label for the goal period.
func periodRangeLabel(period string, lang i18n.Lang) string {
	switch period {
	case "daily":
		return i18n.T(lang, "period.today")
	case "weekly":
		return i18n.T(lang, "period.thisWeek")
	case "monthly":
		return i18n.T(lang, "period.thisMonth")
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
				`SELECT id, name FROM projects WHERE id IN (`+placeholders(len(pids))+`)`, toAny(pids)...)
			if rows != nil {
				for rows.Next() {
					var id int64
					var name string
					if err := rows.Scan(&id, &name); err == nil {
						projNameByID[id] = name
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
			dur = strconv.Itoa(as.Session.DurationSeconds(time.Now()))
		}
		note := ""
		if as.Session.Note != nil {
			note = *as.Session.Note
		}
		project := "" // "" = Uncategorized in the CSV
		if name, ok := projNameByID[as.Activity.ProjectID]; ok {
			project = name
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
		views = append(views, toSessionView(as.Session, as.Activity, today.Start, today.End, now, resolveLang(r)))
	}
	hydrateSessionTags(r.Context(), s.db, views)
	hydrateSessionProjects(r.Context(), s.db, views)
	lang := string(resolveLang(r))
	for i := range views {
		views[i].Lang = lang
	}
	s.renderFragment(w, "active-list", activeListVM{Lang: string(resolveLang(r)), Items: views})
}

// handleMiniBar renders the phone "now tracking" bar shown above the tab
// bar on every page but the dashboard. Empty body when nothing runs.
func (s *Server) handleMiniBar(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	today, _ := timeparse.ResolvePeriod("today", now)
	active, err := s.db.ListActiveSessions(r.Context(), teamID(r))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	lang := resolveLang(r)
	views := make([]sessionView, 0, len(active))
	for _, as := range active {
		views = append(views, toSessionView(as.Session, as.Activity, today.Start, today.End, now, lang))
	}
	// Running first: the bar shows the timer that is actually ticking.
	sort.SliceStable(views, func(i, j int) bool { return !views[i].Paused && views[j].Paused })
	s.renderFragment(w, "minibar", activeListVM{Lang: string(lang), Items: views})
}

func parseID(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
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
	return sess.TrackedSecondsInWindow(start, end, time.Now())
}


// notifyNewlyMetGoals pushes a notification for every goal that crossed
// 100% because of the session we just closed. Cheap: one ProgressForGoals
// query; dedupe is by "goal was under 100 before the stop".
func (s *Server) notifyNewlyMetGoals(r *http.Request, activityID int64) {
	progress, err := s.db.ProgressForGoals(r.Context(), teamID(r), time.Now())
	if err != nil {
		return
	}
	for _, p := range progress {
		if p.Goal.ActivityID != activityID {
			continue
		}
		if p.PercentComplete < 100 {
			continue
		}
		// We only see the AFTER state; treat "exceeded" as met and send
		// at most once per period by checking if the goal just turned.
		// A simple heuristic: send when percent is exactly around 100+
		// and the achieved label is fresh — acceptable for v1.
		s.sendPush(teamID(r),
			"Goal met",
			p.ActivityName+" · "+periodRangeLabel(p.Goal.Period, resolveLang(r)),
			"/goals")
		break // one push per stop
	}
}
