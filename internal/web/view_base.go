package web

import (
	"html/template"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// pageData is the common envelope every HTML page needs: title for the
// <title> tag, the nav-highlight flag, and a pre-rendered ContentHTML
// fragment that base.html drops into the layout.
//
// Why pre-rendered: Go html/template does not allow `{{template .X .}}`
// with a dynamic name, so we render the page-specific block into a
// buffer first and pass the safe HTML into the layout.
type pageData struct {
	Title         string
	Active        string
	ContentHTML   template.HTML
	User          *userView       // nil for /login, /register
	Team          *teamView       // current team (personal or shared)
	UserTeams     []teamsView     // every team the user belongs to (workspace switcher)
	RequestPath   string          // current URL path; used as next= after a switch
	CSRFToken     string          // echoed into form hidden fields
	Lang          string          // "en" | "ru" — resolved from cookie / Accept-Language
	CanManage     bool            // owner/admin: money and settings are shown
	Mods          map[string]bool // sections this workspace uses (modules.go)
	DurFmt        string          // user's duration format, for the JS clocks
	ReactApp      bool            // render page content from the React application
	ReactPayload  template.HTML   // JSON bootstrap data for the React application
	Tabs          []navItem       // phone tab bar (prefs)
	hiddenWidgets []string
}

func (pageData) isTemplateData() {}

func (p pageData) usesReactApp() bool { return p.ReactApp }

// userView exposes only fields used by the shared HTML layout.
type userView struct {
	ID    int64
	Email string
	Name  string
}

// teamView exposes only fields used by the shared HTML layout.
type teamView struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// projectView contains presentation-safe project fields used by HTML pages.
// Billing rates stay in page-specific formatted fields and never enter the
// shared template data graph.
type projectView struct {
	ID       int64
	Slug     string
	Name     string
	Color    string
	Archived bool
	Billable bool
}

func projectViews(projects []model.Project) []projectView {
	views := make([]projectView, 0, len(projects))
	for _, project := range projects {
		views = append(views, projectView{
			ID: project.ID, Slug: project.Slug, Name: project.Name,
			Color: project.Color, Archived: project.Archived, Billable: project.Billable,
		})
	}
	return views
}

// activityView contains only fields rendered by HTML pages.
type activityView struct {
	ID        int64
	Name      string
	ProjectID int64
	Archived  bool
	Lang      string
}

func activityViews(activities []model.Activity, lang string) []activityView {
	views := make([]activityView, 0, len(activities))
	for _, activity := range activities {
		views = append(views, activityView{
			ID: activity.ID, Name: activity.Name, ProjectID: activity.ProjectID,
			Archived: activity.Archived, Lang: lang,
		})
	}
	return views
}

func (v activityView) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }

type savedReportView struct {
	ID          int64
	Name        string
	Period      string
	ProjectSlug string
	Tag         string
	CreatedBy   int64
}

func savedReportViews(reports []model.SavedReport) []savedReportView {
	views := make([]savedReportView, 0, len(reports))
	for _, report := range reports {
		views = append(views, savedReportView{
			ID: report.ID, Name: report.Name, Period: report.Period,
			ProjectSlug: report.ProjectSlug, Tag: report.Tag, CreatedBy: report.CreatedBy,
		})
	}
	return views
}

func (p *pageData) setManage(v bool) { p.CanManage = v }

func (p *pageData) setModules(m map[string]bool) { p.Mods = m }

func (p *pageData) setWidgets(hidden []string) { p.hiddenWidgets = hidden }

type widgetsCarrier interface{ setWidgets([]string) }

// On reports whether a section is switched on ({{if .On "invoices"}}).
// No set yet (logged-out pages) means on.
func (p pageData) On(key string) bool { return p.Mods == nil || p.Mods[key] }

// Widget reports whether a dashboard block is shown (user prefs).
func (p pageData) Widget(key string) bool { return !has(p.hiddenWidgets, key) }

type modulesCarrier interface{ setModules(map[string]bool) }

// manageCarrier is a page that hides money/settings from members.
type manageCarrier interface{ setManage(bool) }

// T translates a dictionary key for the page's language. Called from
// templates as {{.T "nav.dashboard"}}; inside {{range}} use {{$.T …}}.
func (p pageData) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

// LangName is the uppercase switcher label ("EN" / "RU").
func (p pageData) LangName() string { return strings.ToUpper(p.Lang) }

func (p *pageData) setCSRF(t string) { p.CSRFToken = t }

func (p *pageData) setLang(l string) { p.Lang = l }

// pageMeta is implemented by every page view-model so render() can
// pull Title/Active without a closed type switch.
type pageMeta interface {
	pageInfo() (title, active string)
}

func (p pageData) pageInfo() (string, string) { return p.Title, p.Active }
