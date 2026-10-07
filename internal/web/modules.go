package web

import (
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// A workspace picks which parts of the app it uses. The ledger core
// (dashboard, stats, timesheet, projects) is always there; the rest is
// switched on per workspace, at onboarding (/welcome) or later
// (/settings/sections). An empty setting means everything, so a
// workspace made before sections existed sees what it always saw.
type moduleDef struct {
	Key, Icon, Label, Hint string // Label/Hint are i18n keys
	Manage                 bool   // only owners/admins use it anyway
}

var modules = []moduleDef{
	{Key: "graph", Icon: "clock", Label: "nav.graph", Hint: "mod.graph"},
	{Key: "goals", Icon: "flag", Label: "nav.goals", Hint: "mod.goals"},
	{Key: "tags", Icon: "tag", Label: "nav.tags", Hint: "mod.tags"},
	{Key: "invoices", Icon: "zap", Label: "nav.invoices", Hint: "mod.invoices", Manage: true},
	{Key: "reports", Icon: "bar-chart-3", Label: "nav.reports", Hint: "mod.reports", Manage: true},
	{Key: "payroll", Icon: "users", Label: "nav.payroll", Hint: "mod.payroll", Manage: true},
	{Key: "schedule", Icon: "calendar", Label: "nav.schedule", Hint: "mod.schedule"},
	{Key: "integrations", Icon: "link", Label: "nav.integrations", Hint: "mod.integrations"},
	{Key: "import", Icon: "download", Label: "nav.import", Hint: "mod.import"},
}

// sectionsCounts reports how many sections the workspace has open and how many
// the catalogue holds. The dashboard shows it so the mode line says what the
// mode means — "5 of 9 sections" — instead of only naming it.
func sectionsCounts(enabled map[string]bool) (open, total int) {
	for _, m := range modules {
		total++
		if enabled[m.Key] {
			open++
		}
	}
	return open, total
}

// presets are the onboarding choices; "all" is the studio.
var presets = []struct {
	Key, Title, Blurb, Icon string
	Modules                 []string
}{
	{"solo", "preset.solo", "preset.soloBlurb", "user", []string{"graph", "goals", "tags", "integrations", "import"}},
	{"freelance", "preset.freelance", "preset.freelanceBlurb", "zap", []string{"graph", "goals", "tags", "invoices", "reports", "integrations", "import"}},
	{"studio", "preset.studio", "preset.studioBlurb", "users", []string{"graph", "goals", "tags", "invoices", "reports", "payroll", "schedule", "integrations", "import"}},
}

// The active mode is the exact workspace section preset. Personal menu
// preferences do not change it; a manually changed set is custom.
func presetKeyForModules(enabled map[string]bool) string {
	encoded := appmodel.EncodeSections(enabled)
	for _, preset := range presets {
		selected := make(map[string]bool, len(preset.Modules))
		for _, key := range preset.Modules {
			selected[key] = true
		}
		if appmodel.EncodeSections(selected) == encoded {
			return preset.Key
		}
	}
	return "custom"
}

// userModules is what this person's menu shows: the workspace's sections
// minus the ones they hid for themselves.
func (s *Server) userModules(r *http.Request) map[string]bool {
	on := s.teamModules(r)
	for _, k := range prefsOf(r).HiddenSections {
		delete(on, k)
	}
	return on
}

type teamModulesCache struct {
	once    sync.Once
	enabled map[string]bool
}

func (c *teamModulesCache) load(read func() (map[string]bool, error)) map[string]bool {
	c.once.Do(func() {
		var err error
		c.enabled, err = read()
		if err != nil {
			c.enabled = map[string]bool{}
		}
	})
	return copyModuleSet(c.enabled)
}

func copyModuleSet(source map[string]bool) map[string]bool {
	if source == nil {
		return nil
	}
	cloned := make(map[string]bool, len(source))
	for key, enabled := range source {
		cloned[key] = enabled
	}
	return cloned
}

func (s *Server) teamModules(r *http.Request) map[string]bool {
	read := func() (map[string]bool, error) {
		return s.services.Teams.Settings.SectionModules(r.Context(), teamID(r))
	}
	if cache, ok := r.Context().Value(ctxTeamModulesKey).(*teamModulesCache); ok && cache != nil {
		return cache.load(read)
	}
	enabled, err := read()
	if err != nil {
		return map[string]bool{} // fail closed: storage failure must not enable gated sections
	}
	return copyModuleSet(enabled)
}

// module guards a section's routes: switched off, its pages lead to the
// place where it can be switched back on (or home, for a member).
func (s *Server) module(key string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.teamModules(r)[key] {
			h(w, r)
			return
		}
		msg := i18n.T(resolveLang(r), "mod.off")
		if r.Method != http.MethodGet || strings.HasPrefix(r.URL.Path, "/api/") {
			http.Error(w, msg, http.StatusNotFound)
			return
		}
		to := "/"
		if canManage(r) {
			to = "/settings/sections"
		}
		http.Redirect(w, r, to+"?flash="+url.QueryEscape(encodeFlash(false, msg)), http.StatusSeeOther)
	}
}

type moduleView struct {
	Key, Icon, Label, Hint string
	On, Manage             bool
}

type presetView struct {
	Key, Title, Blurb, Icon string
	Active                  bool // the current set is exactly this preset
	Names                   []string
}

type sectionsPage struct {
	pageData
	SectionsReact bool
	Modules       []moduleView
	Presets       []presetView
	Welcome       bool // the onboarding step right after sign-up
	Flash         string
	FlashOK       bool
}

func (p *sectionsPage) setCSRF(t string) { p.pageData.setCSRF(t) }

func (s *Server) sectionsData(r *http.Request, welcome bool) *sectionsPage {
	lang := resolveLang(r)
	on := s.teamModules(r)
	title, active := "Sections", "settings-sections"
	if welcome {
		title, active = "Welcome", "welcome"
	}
	data := &sectionsPage{pageData: pageData{Title: title, Active: active, Lang: string(lang), ReactApp: true}, SectionsReact: true, Welcome: welcome}
	for _, m := range modules {
		data.Modules = append(data.Modules, moduleView{Key: m.Key, Icon: m.Icon, Label: m.Label, Hint: m.Hint, On: on[m.Key], Manage: m.Manage})
	}
	for _, p := range presets {
		pv := presetView{Key: p.Key, Title: p.Title, Blurb: p.Blurb, Icon: p.Icon}
		set := map[string]bool{}
		for _, k := range p.Modules {
			set[k] = true
			for _, m := range modules {
				if m.Key == k {
					pv.Names = append(pv.Names, i18n.T(lang, m.Label))
				}
			}
		}
		pv.Active = !welcome && appmodel.EncodeSections(set) == appmodel.EncodeSections(on)
		data.Presets = append(data.Presets, pv)
	}
	if f := r.URL.Query().Get("flash"); f != "" {
		data.Flash, data.FlashOK = decodeFlash(f, lang)
	}
	return data
}

// handleWelcome is the one onboarding question: how will you use it?
func (s *Server) handleWelcome(w http.ResponseWriter, r *http.Request) {
	if !canManage(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderPageForRequest(w, r, "Welcome", "welcome", "sections", s.sectionsData(r, true))
}

func (s *Server) handleSectionsPage(w http.ResponseWriter, r *http.Request) {
	s.renderPageForRequest(w, r, "Sections", "settings-sections", "sections", s.sectionsData(r, false))
}

// handleAPITeamModules saves a preset or a hand-picked list.
func (s *Server) handleAPITeamModules(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/sections?flash=bad_request", http.StatusSeeOther)
		return
	}
	on := map[string]bool{}
	if pk := r.PostForm.Get("preset"); pk != "" {
		for _, p := range presets {
			if p.Key == pk {
				for _, k := range p.Modules {
					on[k] = true
				}
			}
		}
	} else {
		for _, k := range r.PostForm["modules"] {
			if appmodel.IsKnownSection(k) {
				on[k] = true
			}
		}
	}
	if err := s.services.Teams.Settings.UpdateModules(r.Context(), appmodel.TeamModulesRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), Selected: on}); err != nil {
		http.Redirect(w, r, "/settings/sections?flash="+url.QueryEscape(encodeFlash(false, s.teamErrorMessage(r, err))), http.StatusSeeOther)
		return
	}
	encoded := appmodel.EncodeSections(on)
	s.audit(r, "team.modules", strings.ReplaceAll(encoded, ",", ", "), "")
	if r.PostForm.Get("from") == "welcome" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/sections?flash=updated", http.StatusSeeOther)
}
