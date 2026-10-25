package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// searchResponse is the global search's answer: the groups that have matches, each row
// with the page that opens it (UI-01).
type searchResponse struct {
	Groups []searchGroup `json:"groups"`
}

func (searchResponse) isJSONResponse() {}

// searchGroup is one kind of match. Kind is the stable name of the kind ("clients",
// "contacts"), so a screen can pick the groups it shows without reading the label.
type searchGroup struct {
	Kind  string       `json:"kind"`
	Label string       `json:"label"`
	Items []searchItem `json:"items"`
}

type searchItem struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Href   string `json:"href"`
}

// handleSearch answers the global search for one text (UI-01). The text only filters:
// every row links to a page the viewer already has, and people's emails come back to
// managers alone. The optional tag keeps the clients that carry it, and their contacts.
// limit sets the rows per kind, up to 50; the default is eight, which is what the menu
// shows (CRM-18).
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	results, err := s.services.Search.Find(r.Context(), appmodel.SearchQuery{
		TeamID: teamID(r), UserID: authenticatedUserID(r), Text: r.URL.Query().Get("q"),
		Tag: r.URL.Query().Get("tag"), Limit: limit, CRM: s.teamModules(r)["crm"],
	})
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	out := searchResponse{Groups: []searchGroup{}}
	addGroup := func(key string, items []searchItem) {
		if len(items) == 0 {
			return
		}
		out.Groups = append(out.Groups, searchGroup{Kind: strings.TrimPrefix(key, "search."), Label: i18n.T(lang, key), Items: items})
	}
	hits := func(key string, list []appmodel.SearchHit, href func(appmodel.SearchHit) string, detail func(appmodel.SearchHit) string) {
		items := make([]searchItem, 0, len(list))
		for _, hit := range list {
			items = append(items, searchItem{Title: hit.Title, Detail: detail(hit), Href: href(hit)})
		}
		addGroup(key, items)
	}
	noDetail := func(appmodel.SearchHit) string { return "" }
	id := func(hit appmodel.SearchHit) string { return strconv.FormatInt(hit.ID, 10) }
	hits("search.projects", results.Projects, func(hit appmodel.SearchHit) string { return "/projects/" + url.PathEscape(hit.Slug) }, noDetail)
	hits("search.clients", results.Clients, func(hit appmodel.SearchHit) string { return "/clients/" + id(hit) }, clientSearchDetail)
	contacts := make([]searchItem, 0, len(results.Contacts))
	for _, c := range results.Contacts {
		contacts = append(contacts, searchItem{
			Title:  c.Name,
			Detail: joinSearchDetail(c.ClientName, c.Role, c.Phone, c.Email),
			Href:   "/clients/" + strconv.FormatInt(c.ClientID, 10),
		})
	}
	addGroup("search.contacts", contacts)
	hits("search.tasks", results.Tasks, func(hit appmodel.SearchHit) string { return "/tasks/" + id(hit) }, func(hit appmodel.SearchHit) string { return hit.Detail })
	hits("search.tags", results.Tags, func(hit appmodel.SearchHit) string { return "/stats?tag=" + url.QueryEscape(hit.Title) }, noDetail)
	hits("search.people", results.People, func(appmodel.SearchHit) string { return "/settings/members" }, func(hit appmodel.SearchHit) string { return hit.Detail })
	hits("search.sessions", results.Sessions, func(hit appmodel.SearchHit) string { return "/stats?log=all&session=" + id(hit) }, func(hit appmodel.SearchHit) string {
		return fmtWhen(lang, hit.At.In(userLoc(r)), userNow(r))
	})
	s.writeJSON(w, out)
}

// clientSearchDetail is a client's billing email (managers only, D-2) and its tags.
func clientSearchDetail(hit appmodel.SearchHit) string {
	return joinSearchDetail(append([]string{hit.Detail}, hit.Tags...)...)
}

// joinSearchDetail joins the parts that are filled in. A comma, not a dot, separates
// them: the line is a list of facts, not a sentence.
func joinSearchDetail(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, ", ")
}
