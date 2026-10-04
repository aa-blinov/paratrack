package providers

import (
	"context"
	"net/url"
)

func (c *Client) fetchTodoistTasks(ctx context.Context, token, projectID string) ([]extItem, error) {
	var out []extItem
	cursor := ""
	for len(out) < maxImportTasks {
		u := "https://api.todoist.com/api/v1/tasks?limit=200"
		if projectID != "" {
			u += "&project_id=" + url.QueryEscape(projectID)
		}
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		var raw struct {
			Results []struct {
				ID      string `json:"id"`
				Content string `json:"content"`
				URL     string `json:"url"`
			} `json:"results"`
			NextCursor *string `json:"next_cursor"`
		}
		if _, err := c.getJSON(ctx, "todoist", newReq("GET", u, "", "Authorization", "Bearer "+token), &raw); err != nil {
			return nil, err
		}
		for _, t := range raw.Results {
			link := t.URL
			if link == "" {
				link = "https://app.todoist.com/app/task/" + t.ID
			}
			out = append(out, extItem{ID: "todoist-" + t.ID, Title: t.Content, URL: link, Status: "open"})
		}
		if raw.NextCursor == nil || *raw.NextCursor == "" {
			break
		}
		cursor = *raw.NextCursor
	}
	return out, nil
}
