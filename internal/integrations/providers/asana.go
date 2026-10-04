package providers

import (
	"context"
	"fmt"
	"net/url"
)

func (c *Client) fetchAsanaTasks(ctx context.Context, token, projectGID string) ([]extItem, error) {
	if projectGID == "" {
		return nil, fmt.Errorf("asana project gid is required")
	}
	var out []extItem
	offset := ""
	for len(out) < maxImportTasks {
		u := fmt.Sprintf("https://app.asana.com/api/1.0/projects/%s/tasks?opt_fields=name,completed,permalink_url&completed_since=now&limit=100",
			url.PathEscape(projectGID))
		if offset != "" {
			u += "&offset=" + url.QueryEscape(offset)
		}
		var raw struct {
			Data []struct {
				GID       string `json:"gid"`
				Name      string `json:"name"`
				Completed bool   `json:"completed"`
				Permalink string `json:"permalink_url"`
			} `json:"data"`
			NextPage *struct {
				Offset string `json:"offset"`
			} `json:"next_page"`
		}
		if _, err := c.getJSON(ctx, "asana", newReq("GET", u, "", "Authorization", "Bearer "+token, "Accept", "application/json"), &raw); err != nil {
			return nil, err
		}
		for _, t := range raw.Data {
			if !t.Completed {
				out = append(out, extItem{ID: "asana-" + t.GID, Title: t.Name, URL: t.Permalink, Status: "open"})
			}
		}
		if raw.NextPage == nil || raw.NextPage.Offset == "" {
			break
		}
		offset = raw.NextPage.Offset
	}
	return out, nil
}

// fetchGitLabIssues lists opened issues of "group/project". per_page ≤ 100,
// pages via Link rel="next". Self-hosted default comes from Config.GitLabSite.
// https://docs.gitlab.com/api/issues/ · https://docs.gitlab.com/api/rest/
