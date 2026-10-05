package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// Prefs is how one person likes the app, on top of what their workspace
// switched on. Stored as JSON on the user; zero values are the defaults.
type Prefs = appmodel.UserPreferences

type prefsKey struct{}

func withPrefs(ctx context.Context, p Prefs) context.Context {
	return context.WithValue(ctx, prefsKey{}, p)
}

func prefsOf(r *http.Request) Prefs {
	if r == nil {
		return Prefs{}
	}
	p, _ := r.Context().Value(prefsKey{}).(Prefs)
	return p
}

func has(list []string, k string) bool {
	for _, v := range list {
		if v == k {
			return true
		}
	}
	return false
}

func durFmtOf(r *http.Request) string { return prefsOf(r).Duration }

func weekStartsSunday(r *http.Request) bool { return prefsOf(r).WeekStart == "sun" }

func defaultProject(p Prefs, teamID int64) int64 {
	return p.DefaultProject[strconv.FormatInt(teamID, 10)]
}

// widgetOn: a dashboard block the user hasn't switched off.
func widgetOn(r *http.Request, key string) bool { return !has(prefsOf(r).HiddenWidgets, key) }

// navItem is a place the phone tab bar can point to.
type navItem struct {
	Key    string
	Href   string
	Icon   string
	Label  string
	Module string // "" = core, always available
	Manage bool
}

var navItems = []navItem{
	{"dashboard", "/", "activity", "nav.dashboard", "", false},
	{"stats", "/stats", "bar-chart-3", "nav.stats", "", false},
	{"timesheet", "/timesheet", "calendar", "nav.timesheet", "", false},
	{"projects", "/projects", "folder", "nav.projects", "", false},
	{"graph", "/graph", "clock", "nav.graph", "graph", false},
	{"goals", "/goals", "flag", "nav.goals", "goals", false},
	{"tags", "/tags", "tag", "nav.tags", "tags", false},
	{"invoices", "/invoices", "zap", "nav.invoices", "invoices", true},
	{"reports", "/reports", "bar-chart-3", "nav.reports", "reports", true},
	{"payroll", "/payroll", "users", "nav.payroll", "payroll", true},
	{"schedule", "/schedule", "calendar", "nav.schedule", "schedule", false},
	{"integrations", "/integrations", "link", "nav.integrations", "integrations", false},
}

var defaultTabs = []string{"dashboard", "stats", "timesheet", "projects"}

// tabsFor is the user's phone tab bar: their picks that are still
// available to them, topped up from the defaults to four.
func (s *Server) tabsFor(r *http.Request, mods map[string]bool) []navItem {
	avail := func(n navItem) bool {
		return (n.Module == "" || mods[n.Module]) && (!n.Manage || canManage(r))
	}
	var out []navItem
	seen := map[string]bool{}
	add := func(k string) {
		for _, n := range navItems {
			if n.Key == k && !seen[k] && avail(n) && len(out) < 4 {
				out = append(out, n)
				seen[k] = true
			}
		}
	}
	for _, k := range prefsOf(r).Tabs {
		add(k)
	}
	for _, k := range defaultTabs {
		add(k)
	}
	return out
}

// ---------- /settings/preferences ----------------------------------

var widgetKeys = []string{"unbilled", "goals", "recent", "backfill"}

// zones is the manual time zone menu: Russia first, then the usual places.
var zones = []string{
	"Europe/Kaliningrad", "Europe/Moscow", "Europe/Samara", "Asia/Yekaterinburg", "Asia/Omsk",
	"Asia/Novosibirsk", "Asia/Krasnoyarsk", "Asia/Irkutsk", "Asia/Yakutsk", "Asia/Vladivostok",
	"Asia/Magadan", "Asia/Kamchatka", "Europe/Minsk", "Asia/Almaty", "Asia/Tashkent", "Asia/Tbilisi",
	"Asia/Yerevan", "Asia/Baku", "Europe/Istanbul", "Asia/Dubai", "Asia/Bangkok", "Asia/Bali",
	"Europe/London", "Europe/Berlin", "Europe/Belgrade", "Europe/Lisbon", "America/New_York",
	"America/Los_Angeles", "UTC",
}

type prefCheck struct {
	Key, Icon, Label string
	On               bool
}

type prefsPage struct {
	pageData
	PrefsReact bool
	Sections   []prefCheck // workspace sections, On = shown to me
	TabOpts    []prefCheck // On = in my phone tab bar
	Widgets    []prefCheck
	P          Prefs
	Zones      []string
	Projects   []projectView
	DefProj    int64
	Flash      string
	FlashOK    bool
}

func (p *prefsPage) setCSRF(t string) { p.pageData.setCSRF(t) }

func (s *Server) handlePreferencesPage(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	p := prefsOf(r)
	data := &prefsPage{pageData: pageData{Title: "Preferences", Active: "settings-prefs", Lang: string(lang), ReactApp: true}, PrefsReact: true, P: p, Zones: zones}
	team := s.teamModules(r)
	for _, m := range modules {
		if team[m.Key] && (!m.Manage || canManage(r)) {
			data.Sections = append(data.Sections, prefCheck{m.Key, m.Icon, m.Label, !has(p.HiddenSections, m.Key)})
		}
	}
	tabs := map[string]bool{}
	for _, n := range s.tabsFor(r, s.userModules(r)) {
		tabs[n.Key] = true
	}
	for _, n := range navItems {
		if (n.Module == "" || team[n.Module]) && (!n.Manage || canManage(r)) {
			data.TabOpts = append(data.TabOpts, prefCheck{n.Key, n.Icon, n.Label, tabs[n.Key]})
		}
	}
	for _, k := range widgetKeys {
		data.Widgets = append(data.Widgets, prefCheck{k, "", "widget." + k, widgetOn(r, k)})
	}
	projects, err := s.services.Projects.Queries.List(r.Context(), appmodel.ProjectCatalogQuery{TeamID: teamID(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data.Projects = projectViews(projects)
	data.DefProj = defaultProject(p, teamID(r))
	if f := r.URL.Query().Get("flash"); f != "" {
		data.Flash, data.FlashOK = decodeFlash(f, lang)
	}
	s.renderPageForRequest(w, r, "Preferences", "settings-prefs", "preferences", data)
}

// handleAPIPreferences saves the whole form. Sections the workspace has
// off aren't on the form, so an earlier personal choice about them is kept.
func (s *Server) handleAPIPreferences(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/preferences?flash=bad_request", http.StatusSeeOther)
		return
	}
	f := r.PostForm
	p := prefsOf(r)
	shown := map[string]bool{}
	for _, k := range f["sections"] {
		shown[k] = true
	}
	onForm := map[string]bool{}
	for _, k := range f["sections_all"] {
		onForm[k] = true
	}
	var hidden []string
	for _, m := range modules {
		if (onForm[m.Key] && !shown[m.Key]) || (!onForm[m.Key] && has(p.HiddenSections, m.Key)) {
			hidden = append(hidden, m.Key)
		}
	}
	p.HiddenSections = hidden
	p.Tabs = nil
	for _, k := range f["tabs"] {
		for _, n := range navItems {
			if n.Key == k && len(p.Tabs) < 4 {
				p.Tabs = append(p.Tabs, k)
			}
		}
	}
	switch v := f.Get("duration"); v {
	case "hm", "decimal", "clock":
		p.Duration = v
	default:
		p.Duration = ""
	}
	p.WeekStart = ""
	if f.Get("week_start") == "sun" {
		p.WeekStart = "sun"
	}
	p.TZ = ""
	if tz := f.Get("tz"); has(zones, tz) {
		p.TZ = tz
	}
	p.HiddenWidgets = nil
	for _, k := range widgetKeys {
		if !has(f["widgets"], k) {
			p.HiddenWidgets = append(p.HiddenWidgets, k)
		}
	}
	tid := strconv.FormatInt(teamID(r), 10)
	if p.DefaultProject == nil {
		p.DefaultProject = map[string]int64{}
	}
	delete(p.DefaultProject, tid)
	if id, _ := strconv.ParseInt(f.Get("default_project"), 10, 64); id > 0 {
		p.DefaultProject[tid] = id
	}
	user, _ := UserFrom(r.Context())
	if err := s.services.Preferences.Save(r.Context(), appmodel.PreferencesSaveRequest{UserID: user.ID, CallerID: user.ID, TeamID: teamID(r), Preferences: p}); err != nil {
		msg := i18n.T(resolveLang(r), "err.internal")
		if errors.Is(err, appmodel.ErrInvalidDefaultProject) || errors.Is(err, appmodel.ErrInvalidPreferences) {
			msg = i18n.T(resolveLang(r), "err.invalidInput")
		} else {
			s.logInternalError(err)
		}
		http.Redirect(w, r, "/settings/preferences?flash="+url.QueryEscape(encodeFlash(false, msg)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/preferences?flash=updated", http.StatusSeeOther)
}
