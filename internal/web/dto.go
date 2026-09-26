package web

import (
	"context"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/auth"
	"github.com/aa-blinov/paratrack/internal/i18n"
	dbpkg "github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/teams"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// pageData is the common envelope every HTML page needs: title for the
// <title> tag, the nav-highlight flag, and a pre-rendered ContentHTML
// fragment that base.html drops into the layout.
//
// Why pre-rendered: Go html/template does not allow `{{template .X .}}`
// with a dynamic name, so we render the page-specific block into a
// buffer first and pass the safe HTML into the layout.
type pageData struct {
	Title       string
	Active      string
	ContentHTML template.HTML
	User        *auth.User   // nil for /login, /register
	Team        *teams.Team  // current team (personal or shared)
	UserTeams   []teamsView   // every team the user belongs to (workspace switcher)
	RequestPath string          // current URL path; used as next= after a switch
	CSRFToken   string          // echoed into form hidden fields
	Lang        string          // "en" | "ru" — resolved from cookie / Accept-Language
}

// T translates a dictionary key for the page's language. Called from
// templates as {{.T "nav.dashboard"}}; inside {{range}} use {{$.T …}}.
func (p pageData) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

// LangName is the uppercase switcher label ("EN" / "RU").
func (p pageData) LangName() string { return strings.ToUpper(p.Lang) }

// csrfCarrier is implemented by every page view-model that embeds
// pageData. renderPageForRequest stamps the per-request CSRF token on
// it so `{{.CSRFToken}}` works inside content templates.
type csrfCarrier interface{ setCSRF(string) }

func (p *pageData) setCSRF(t string) { p.CSRFToken = t }

// langCarrier lets the renderers stamp the resolved language.
type langCarrier interface{ setLang(string) }

func (p *pageData) setLang(l string) { p.Lang = l }

// pageMeta is implemented by every page view-model so render() can
// pull Title/Active without a closed type switch.
type pageMeta interface {
	pageInfo() (title, active string)
}

func (p pageData) pageInfo() (string, string) { return p.Title, p.Active }

// teamsView is the minimal row the workspace switcher dropdown needs:
// team identity, role, and id (for the form post).
type teamsView struct {
	ID   int64
	Name string
	Role string
}

// sessionView is the per-row representation of an active or recent
// session in the dashboard / stats tables.
type sessionView struct {
	ID                 int64
	ActivityID         int64
	ActivityName       string
	Color              string
	ProjectID          int64  // 0 if activity has no project
	ProjectName        string // empty if no project
	ProjectColor       string // empty if no project
	ProjectSlug        string // empty if no project
	StartISO           string
	StartLocal         string
	EndLocal           string
	StartInput         string // value for datetime-local
	EndInput           string
	Duration           string
	DurationSecs       int    // clipped tracked seconds behind Duration — aggregate from this, never parse the label
	DurationInput      string // user-editable representation ("1h 30m")
	AccumulatedSeconds int
	Paused             bool
	Note               string
	Tags               []tagChip // attached tags, populated by hydrateSessionTags
	Lang               string    // i18n for fragment templates (session-row, active-list)
}

// T translates a dictionary key. Fragment templates call {{.T "key"}}.
func (v sessionView) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }

// dashboardData feeds dashboard.html.
type dashboardData struct {
	pageData
	Activities []model.Activity
	Projects   []model.Project // for the project picker on the start form
	ActiveSessions []sessionView
	Recent     []sessionView
	ActiveCount int
	TodayTotal string
	TopToday   string
	Goals      []goalView
	ActiveVM   activeListVM // wrapper so active-list can call {{.T}}
	GoalsVM    goalsListVM  // wrapper so goals-list can call {{.T}}

	// First-run onboarding checklist. Shown while the account has no
	// sessions and the visitor has not dismissed it.
	ShowOnboard bool
	HasProject  bool
	HasSession  bool
}

// statsData feeds stats.html.
type statsData struct {
	pageData
	Period       timeparse.Period
	Aggregated   []aggRow        // activity-level breakdown
	ByProject    []projectAggRow // project-grouped breakdown (with activities nested)
	Projects     []model.Project // for the project-filter chip row
	ProjectFilter string         // current ?project=slug value, empty if unfiltered
	Sessions     []sessionView
	Total        string
	SessionCount int
	TagFilter    string // current ?tag= value, empty if unfiltered
	AllTagNames  []string // for the inline-add input autocomplete
	SavedReports []dbpkg.SavedReport
}

type aggRow struct {
	ActivityName string
	Color        string
	Duration     string
	Share        float64
}

// projectAggRow groups the activity-level breakdown under one
// project, so /stats can show "EORA RAG (45%)" with the activities
// nested underneath. Uncategorized activities appear in a single
// row with ProjectID == 0 and an empty Name.
type projectAggRow struct {
	ProjectID   int64
	ProjectName string // empty for "Uncategorized"
	Slug        string
	Color       string
	Duration    string
	Share       float64
	Activities  []aggRow
}

// graphData feeds graph.html.
type graphData struct {
	pageData
	Period    timeparse.Period
	Chart     ChartData
	ChartJSON string // pre-serialised JSON for the data-chart attribute
}

// tagChip is the lightweight view-model for a tag in the stats row
// and on the /tags management page. We don't need the timestamps here.
type tagChip struct {
	ID   int64
	Name string
}

// tagsData feeds tags.html.
type tagsData struct {
	pageData
	Tags         []tagWithCount
	AllTagNames []string // for autocomplete on the new-tag input
}

// tagWithCount is a tag plus how many sessions carry it.
type tagWithCount struct {
	tagChip
	SessionCount int
	Lang         string
}

func (v tagWithCount) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }

// goalView is the per-row representation of a configured goal plus
// the progress actually achieved in its current window. Powers both the
// dashboard widget and the /goals management page.
type goalView struct {
	ID                int64
	ActivityName      string
	Color             string
	Period            string
	TargetMinutes     int
	TargetLabel       string // "2h", "1h 30m"
	AchievedMinutes   int
	AchievedLabel     string // "1h 32m"
	Percent           int    // 0..100+
	AchievedClass     string // "" | "met" | "exceeded"
	PeriodStartLabel  string // "Mon Sep 22"
	PeriodEndLabel    string // "Sun Sep 28"
	PeriodRangeLabel  string // short label e.g. "this week"
	Lang              string
}

func (v goalView) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }

// -- view-model helpers ----------------------------------------------

func toSessionView(s model.Session, a model.Activity, periodStart, periodEnd time.Time, now time.Time) sessionView {
	v := sessionView{
		ID:                 s.ID,
		ActivityID:         s.ActivityID,
		ActivityName:       a.Name,
		Color:              colorFor(a.Name),
		ProjectID:          a.ProjectID,
		StartISO:           s.StartAt.UTC().Format(time.RFC3339Nano),
		StartLocal:         s.StartAt.Local().Format("01-02 15:04"),
		AccumulatedSeconds: s.AccumulatedSeconds,
		Paused:             s.Paused,
	}
	if s.EndAt != nil {
		v.EndLocal = s.EndAt.Local().Format("01-02 15:04")
		v.EndInput = toLocalInput(*s.EndAt)
	}
	v.StartInput = toLocalInput(s.StartAt)
	if s.Note != nil {
		v.Note = *s.Note
	}
	// Compute the FULL duration first (for the editable input); the
	// clipped `Duration` (shown in the table cell) is derived after.
	if s.EndAt != nil {
		fullSecs := s.DurationSeconds(now)
		v.DurationInput = fmtDuration(fullSecs)
		v.DurationSecs = s.TrackedSecondsInWindow(periodStart, periodEnd, now)
		v.Duration = fmtDuration(v.DurationSecs)
	} else if s.LastResumeAt != nil && !s.Paused {
		secs := s.DurationSeconds(now)
		v.DurationSecs = s.TrackedSecondsInWindow(periodStart, periodEnd, now)
		v.Duration = fmtDuration(v.DurationSecs)
		v.DurationInput = durationToHuman(secs)
	} else if s.Paused {
		v.DurationSecs = s.TrackedSecondsInWindow(periodStart, periodEnd, now)
		v.Duration = fmtDuration(v.DurationSecs)
		v.DurationInput = durationToHuman(s.AccumulatedSeconds)
	} else {
		v.Duration = "0m"
		v.DurationInput = "0m"
	}
	return v
}

// hydrateSessionTags does a single batched lookup and attaches the
// resulting tag chips to each row. Safe to call with an empty slice.
func hydrateSessionTags(ctx context.Context, d *dbpkg.DB, rows []sessionView) {
	if len(rows) == 0 {
		return
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	tagsByID, err := d.TagsForSessions(ctx, ids)
	if err != nil {
		return // non-fatal — just skip rendering tags
	}
	for i := range rows {
		for _, t := range tagsByID[rows[i].ID] {
			rows[i].Tags = append(rows[i].Tags, tagChip{ID: t.ID, Name: t.Name})
		}
	}
}

// projectChip is the lightweight project view-model used in session
// rows. Same shape as tagChip; distinct type so templates can tell
// them apart if they want different rendering.
type projectChip struct {
	ID    int64
	Name  string
	Slug  string
	Color string
}

// hydrateSessionProjects looks up the project for each session's
// activity (a session inherits its activity's project) and writes
// the chip onto the row. Sessions whose activity has no project are
// left blank — they show as "Uncategorized" in the badge if the
// template chooses to render that.
func hydrateSessionProjects(ctx context.Context, d *dbpkg.DB, rows []sessionView) {
	if len(rows) == 0 {
		return
	}
	// One query: every distinct (project_id) across the rows.
	seen := map[int64]struct{}{}
	pids := []int64{}
	for _, r := range rows {
		if r.ProjectID == 0 {
			continue
		}
		if _, ok := seen[r.ProjectID]; ok {
			continue
		}
		seen[r.ProjectID] = struct{}{}
		pids = append(pids, r.ProjectID)
	}
	if len(pids) == 0 {
		return
	}
	rows2, err := d.SQL().QueryContext(ctx,
		`SELECT id, slug, name, color FROM projects WHERE id IN (`+placeholders(len(pids))+`)`,
		toAny(pids)...)
	if err != nil {
		return
	}
	defer rows2.Close()
	byID := map[int64]projectChip{}
	for rows2.Next() {
		var p projectChip
		if err := rows2.Scan(&p.ID, &p.Slug, &p.Name, &p.Color); err == nil {
			byID[p.ID] = p
		}
	}
	for i, r := range rows {
		if p, ok := byID[r.ProjectID]; ok {
			rows[i].ProjectName = p.Name
			rows[i].ProjectColor = p.Color
			rows[i].ProjectSlug = p.Slug
		}
	}
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, 2*n)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}

func toAny(xs []int64) []any {
	out := make([]any, len(xs))
	for i, x := range xs {
		out[i] = x
	}
	return out
}

// durationToHuman is an alias of fmtDuration so the editable field
// matches the read-only cells.
func durationToHuman(sec int) string { return fmtDuration(sec) }

// fmtDuration is the single duration label used everywhere.
func fmtDuration(sec int) string {
	if sec <= 0 {
		return "0m"
	}
	if sec < 60 {
		return "1m"
	}
	h := sec / 3600
	m := (sec / 60) % 60
	switch {
	case h > 0 && m > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case h > 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func toLocalInput(t time.Time) string {
	// datetime-local wants "2006-01-02T15:04" with no zone and no seconds.
	return t.Local().Format("2006-01-02T15:04")
}

// shortSummary joins names with a comma for the dashboard's "Top today" line.
func shortSummary(name string) string {
	if name == "" {
		return ""
	}
	return strings.TrimSpace(name)
}


// Fragment wrappers give {{.T}} a language even when the item list is
// empty (a bare []tagWithCount has no method to call).

type tagsListVM struct {
	Lang string
	Tags []tagWithCount
}

func (v tagsListVM) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }

type goalsListVM struct {
	Lang  string
	Goals []goalView
}

func (v goalsListVM) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }

type activeListVM struct {
	Lang  string
	Items []sessionView // active-list ranges over Items
}

func (v activeListVM) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }
