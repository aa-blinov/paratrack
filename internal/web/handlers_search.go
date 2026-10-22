package web

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// searchResponse is the global search's answer: the groups that have matches, each row
// with the page that opens it (UI-01).
type searchResponse struct {
	Groups []searchGroup `json:"groups"`
}

func (searchResponse) isJSONResponse() {}

type searchGroup struct {
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
// managers alone.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	lang := resolveLang(r)
	results, err := s.services.Search.Find(r.Context(), appmodel.SearchQuery{
		TeamID: teamID(r), UserID: authenticatedUserID(r), Manager: canManage(r), Text: r.URL.Query().Get("q"),
	})
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	out := searchResponse{Groups: []searchGroup{}}
	add := func(key string, hits []appmodel.SearchHit, href func(appmodel.SearchHit) string, detail func(appmodel.SearchHit) string) {
		if len(hits) == 0 {
			return
		}
		group := searchGroup{Label: i18n.T(lang, key), Items: []searchItem{}}
		for _, hit := range hits {
			group.Items = append(group.Items, searchItem{Title: hit.Title, Detail: detail(hit), Href: href(hit)})
		}
		out.Groups = append(out.Groups, group)
	}
	noDetail := func(appmodel.SearchHit) string { return "" }
	id := func(hit appmodel.SearchHit) string { return strconv.FormatInt(hit.ID, 10) }
	add("search.projects", results.Projects, func(hit appmodel.SearchHit) string { return "/projects/" + url.PathEscape(hit.Slug) }, noDetail)
	add("search.clients", results.Clients, func(hit appmodel.SearchHit) string { return "/clients/" + id(hit) }, func(hit appmodel.SearchHit) string { return hit.Detail })
	add("search.tasks", results.Tasks, func(hit appmodel.SearchHit) string { return "/tasks/" + id(hit) }, func(hit appmodel.SearchHit) string { return hit.Detail })
	add("search.tags", results.Tags, func(hit appmodel.SearchHit) string { return "/stats?tag=" + url.QueryEscape(hit.Title) }, noDetail)
	add("search.people", results.People, func(appmodel.SearchHit) string { return "/settings/members" }, func(hit appmodel.SearchHit) string { return hit.Detail })
	add("search.sessions", results.Sessions, func(hit appmodel.SearchHit) string { return "/stats?log=all&session=" + id(hit) }, func(hit appmodel.SearchHit) string {
		return fmtWhen(lang, hit.At.In(userLoc(r)), userNow(r))
	})
	s.writeJSON(w, out)
}
