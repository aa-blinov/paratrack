package web

import (
	"html/template"
	"net/http"
	"net/url"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------- helpers ------------------------------------------------

type templateData interface {
	isTemplateData()
}

type pageTemplateData interface {
	templateData
	setCSRF(string)
	setLang(string)
}

type fragmentTemplateData interface {
	templateData
	isFragmentTemplateData()
}

// renderPage is the canonical two-step page renderer:
//  1. Execute the page-specific content template into a buffer.
//  2. Wrap the result in the base.html layout, with the buffer as
//     ContentHTML (template.HTML keeps the renderer from re-escaping).
//
// The same data struct is passed to both renders so per-page fields
// like .Period, .Sessions etc. are still in scope when the content
// block runs.
// renderPage renders a public (pre-auth) page and injects request-scoped CSRF
// and language values through the page model's required contract.
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, title, active, contentTpl string, data pageTemplateData) {
	s.renderPageStatus(w, r, http.StatusOK, title, active, contentTpl, data, false)
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
func (s *Server) renderPageForRequest(w http.ResponseWriter, r *http.Request, title, active, contentTpl string, data pageTemplateData) {
	s.renderPageForRequestStatus(w, r, http.StatusOK, title, active, contentTpl, data)
}

func (s *Server) renderPageForRequestStatus(w http.ResponseWriter, r *http.Request, status int, title, active, contentTpl string, data pageTemplateData) {
	s.renderPageStatus(w, r, status, title, active, contentTpl, data, true)
}

// renderPageStatus shares template preparation and layout execution across
// public and authenticated pages. Authenticated pages add request-scoped
// navigation and preference data to the common wrapper.
func (s *Server) renderPageStatus(w http.ResponseWriter, r *http.Request, status int, title, active, contentTpl string, data pageTemplateData, authenticated bool) {
	token := ensureCSRF(w, r)
	lang := resolveLang(r)
	data.setCSRF(token)
	data.setLang(string(lang))
	if m, ok := data.(manageCarrier); ok {
		m.setManage(canManage(r))
	}
	var mods map[string]bool
	if _, ok := TeamFrom(r.Context()); ok {
		if authenticated {
			mods = s.userModules(r)
		} else {
			mods = s.teamModules(r)
		}
		if m, ok := data.(modulesCarrier); ok {
			m.setModules(mods)
		}
	}
	if wc, ok := data.(widgetsCarrier); ok && authenticated {
		wc.setWidgets(prefsOf(r).HiddenWidgets)
	}
	content, err := s.executeTemplate(contentTpl, data)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	wrapper := pageData{
		CanManage:   canManage(r),
		Mods:        mods,
		Title:       pageTitle(lang, title),
		Active:      active,
		ContentHTML: template.HTML(content),
		RequestPath: r.URL.Path,
		CSRFToken:   token,
		Lang:        string(lang),
	}
	if authenticated {
		wrapper.DurFmt = durFmtOf(r)
		wrapper.Tabs = s.tabsFor(r, mods)
		var currentUser appmodel.UserIdentity
		var currentTeam model.Team
		if u, ok := UserFrom(r.Context()); ok {
			currentUser = u
			wrapper.User = &userView{ID: currentUser.ID, Email: currentUser.Email, Name: currentUser.Name}
		}
		if t, ok := TeamFrom(r.Context()); ok {
			currentTeam = t
			wrapper.Team = &teamView{ID: currentTeam.ID, Name: currentTeam.Name, CreatedAt: currentTeam.CreatedAt}
			// Workspace switcher list. Cheap — one indexed lookup.
			memberships, err := s.services.Teams.Directory.MembershipsForUser(r.Context(), currentUser.ID)
			if err != nil {
				s.writeInternalError(w, err)
				return
			}
			for _, membership := range memberships {
				wrapper.UserTeams = append(wrapper.UserTeams, teamsView{
					ID: membership.Team.ID, Name: membership.Team.Name, Role: string(membership.Role),
				})
			}
		}
	}
	page, err := s.executeTemplate("base", wrapper)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(page); err != nil {
		s.logger.Printf("web: write rendered page: %v", err)
	}
}

// renderFragment renders a self-contained template (not wrapped in base).
// Used for HTMX swap targets like active-list and session-row.
func (s *Server) renderFragment(w http.ResponseWriter, name string, data fragmentTemplateData) {
	fragment, err := s.executeTemplate(name, data)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if name == "active-list" {
		// Every timer action lands here; the "Today" card listens and refreshes.
		w.Header().Set("HX-Trigger", "sessions-changed")
	}
	if _, err := w.Write(fragment); err != nil {
		s.logger.Printf("web: write rendered fragment %q: %v", name, err)
	}
}

// teamID returns the authenticated workspace ID. The auth middleware rejects
// requests without a positive workspace; the zero fallback is retained for
// direct handler invocation in isolated tests.
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
func (s *Server) render(w http.ResponseWriter, r *http.Request, contentTpl string, data pageTemplateData) {
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

// ---------- JSON / fragments ---------------------------------------
