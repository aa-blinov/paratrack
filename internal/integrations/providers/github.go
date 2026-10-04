package providers

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/httpurl"
)

func (c *Client) fetchGitHubIssues(ctx context.Context, token, repo string) ([]extItem, error) {
	next := "https://api.github.com/issues?state=open&per_page=100"
	if parts := strings.SplitN(repo, "/", 2); len(parts) == 2 {
		next = fmt.Sprintf("https://api.github.com/repos/%s/%s/issues?state=open&per_page=100",
			url.PathEscape(parts[0]), url.PathEscape(parts[1]))
	}
	var out []extItem
	for next != "" && len(out) < maxImportTasks {
		var raw []struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			HTMLURL string `json:"html_url"`
			State   string `json:"state"`
			Repo    struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			PullRequest *struct{} `json:"pull_request"`
		}
		h, err := c.getJSON(ctx, "github", newReq("GET", next, "",
			"Authorization", "Bearer "+token, "Accept", "application/vnd.github+json",
			"X-GitHub-Api-Version", "2022-11-28", "User-Agent", "paratrack"), &raw)
		if err != nil {
			return nil, err
		}
		for _, i := range raw {
			if i.PullRequest != nil {
				continue
			}
			full, name := i.Repo.FullName, i.Title
			if full != "" {
				name = full + "#" + strconv.Itoa(i.Number) + " " + i.Title
			} else {
				full = repo
			}
			// Numbers repeat across repos: the id carries the repo.
			out = append(out, extItem{ID: fmt.Sprintf("gh-%s#%d", full, i.Number), Title: name, URL: i.HTMLURL, Status: i.State})
		}
		link := linkNext(h)
		if link == "" {
			next = ""
			continue
		}
		next, err = httpurl.ResolveSameOrigin(next, link)
		if err != nil {
			return nil, fmt.Errorf("github pagination link: %w", err)
		}
	}
	return out, nil
}

// fetchTrelloCards lists open cards on a board; secret is "key:token".
// No page param: limit ≤ 1000, older cards via before=<last card id>.
// https://developer.atlassian.com/cloud/trello/guides/rest-api/nested-resources/
