package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
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

// renderFragment renders a self-contained template (not wrapped in base).
// Used for HTMX swap targets like active-list and session-row.
func (s *Server) renderFragment(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "render fragment ["+name+"]: "+err.Error(), http.StatusInternalServerError)
	}
}

// render is kept as a thin wrapper for handlers that want the simpler
// signature; it derives title/active from the embedded pageData.
func (s *Server) render(w http.ResponseWriter, contentTpl string, data any) {
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
	s.renderPage(w, title, active, contentTpl, data)
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
	acts, err := s.db.ListActivities(ctx, false)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	active, err := s.db.ListActiveSessions(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	today, _ := timeparse.ResolvePeriod("today", now)
	recent, err := s.db.ListClosedSessionsInRange(ctx, today.Start, today.End.Add(24*time.Hour), nil)
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
	s.render(w, "dashboard-content", d)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()
	period := s.parsePeriod(r)

	sessions, err := s.db.ListClosedSessionsInRange(ctx, period.Start, period.End, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	agg := map[string]int{}
	total := 0
	rows := make([]sessionView, 0, len(sessions))
	for _, as := range sessions {
		clipped := clipSeconds(as.Session, period.Start, period.End)
		if clipped <= 0 {
			continue
		}
		agg[as.Activity.Name] += clipped
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

	s.render(w, "stats-content", statsData{
		pageData:    pageData{Title: "Stats", Active: "stats"},
		Period:      period,
		Aggregated:  aggs,
		Sessions:    rows,
		Total:       fmtDuration(total),
		SessionCount: len(rows),
	})
}

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	period := s.parsePeriod(r)

	sessions, err := s.db.ListClosedSessionsInRange(ctx, period.Start, period.End, nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	chart := buildChartData(sessions, period)

	chartJSON, _ := json.Marshal(chart)
	s.render(w, "graph-content", graphData{
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
	active, err := s.db.ListActiveSessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views := make([]sessionView, 0, len(active))
	for _, as := range active {
		views = append(views, toSessionView(as.Session, as.Activity, today.Start, today.End, now))
	}
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
	ctx := r.Context()
	act, err := s.db.GetOrCreateActivity(ctx, name)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// Reject duplicate active session for the same activity.
	active, err := s.db.ListActiveSessions(ctx)
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
	if _, err := s.db.CreateSession(ctx, act.ID, time.Now(), note); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.toast(w, "started "+act.Name, "success")
	// HTMX target was the active-list fragment — re-render it with the
	// now-complete active list (including the session we just created).
	fresh, err := s.db.ListActiveSessions(ctx)
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
	act, err := s.db.FindActivityByName(ctx, name)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	active, err := s.db.ListActiveSessions(ctx)
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
		_, _ = s.db.CreateSession(ctx, act.ID, now, "")
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

func (s *Server) handleCSV(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="paratrack.csv"`)
	sessions, err := s.db.ListClosedSessionsInRange(r.Context(),
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), time.Now().Add(24*time.Hour), nil)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	fmt.Fprintln(w, "id,activity,start,end,duration_seconds,note")
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
		fmt.Fprintf(w, "%d,%q,%s,%s,%s,%q\n",
			as.Session.ID, as.Activity.Name,
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
	active, err := s.db.ListActiveSessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	views := make([]sessionView, 0, len(active))
	for _, as := range active {
		views = append(views, toSessionView(as.Session, as.Activity, today.Start, today.End, now))
	}
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
	return int(ee.Sub(se).Seconds())
}
