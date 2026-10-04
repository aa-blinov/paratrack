package providers

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aa-blinov/paratrack/internal/httpurl"
)

func (c *Client) fetchGitLabIssues(ctx context.Context, token, project string) ([]extItem, error) {
	// Self-hosted: the site may lead the target ("https://git.acme.ru group/app").
	site := c.config.GitLabSite
	if f := strings.Fields(project); len(f) == 2 && strings.HasPrefix(strings.ToLower(f[0]), "https://") {
		site, project = f[0], f[1]
	} else if f := strings.Fields(project); len(f) > 0 && strings.Contains(f[0], "://") {
		return nil, ErrInvalidProviderSite
	}
	if site == "" {
		site = "https://gitlab.com"
	}
	var err error
	site, err = validateProviderSite(site)
	if err != nil {
		return nil, fmt.Errorf("gitlab: %w", err)
	}
	if project == "" {
		return nil, fmt.Errorf("gitlab project (group/project) is required")
	}
	next := site + "/api/v4/projects/" + url.PathEscape(project) + "/issues?state=opened&per_page=100"
	var out []extItem
	for next != "" && len(out) < maxImportTasks {
		var raw []struct {
			IID    int    `json:"iid"`
			Title  string `json:"title"`
			WebURL string `json:"web_url"`
			State  string `json:"state"`
		}
		h, err := c.getJSON(ctx, "gitlab", newReq("GET", next, "", "Authorization", "Bearer "+token), &raw)
		if err != nil {
			return nil, err
		}
		for _, i := range raw {
			out = append(out, extItem{ID: fmt.Sprintf("gitlab-%d", i.IID), Title: fmt.Sprintf("#%d %s", i.IID, i.Title), URL: i.WebURL, Status: i.State})
		}
		link := linkNext(h)
		if link == "" {
			next = ""
			continue
		}
		next, err = httpurl.ResolveSameOrigin(next, link)
		if err != nil {
			return nil, fmt.Errorf("gitlab pagination link: %w", err)
		}
	}
	return out, nil
}

// fetchClickUpTasks lists open tasks of a list. page starts at 0, up to
// 100 per page, until last_page. Raw personal token in Authorization.
// https://developer.clickup.com/reference/gettasks
