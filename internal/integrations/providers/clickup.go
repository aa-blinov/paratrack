package providers

import (
	"context"
	"fmt"
	"net/url"
)

func (c *Client) fetchClickUpTasks(ctx context.Context, token, listID string) ([]extItem, error) {
	if listID == "" {
		return nil, fmt.Errorf("clickup list id is required")
	}
	var out []extItem
	for page := 0; len(out) < maxImportTasks; page++ {
		u := fmt.Sprintf("https://api.clickup.com/api/v2/list/%s/task?include_closed=false&order_by=updated&reverse=true&page=%d",
			url.PathEscape(listID), page)
		var raw struct {
			Tasks []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				URL    string `json:"url"`
				Status struct {
					Status string `json:"status"`
					Type   string `json:"type"`
				} `json:"status"`
			} `json:"tasks"`
			LastPage *bool `json:"last_page"`
		}
		if _, err := c.getJSON(ctx, "clickup", newReq("GET", u, "", "Authorization", token), &raw); err != nil {
			return nil, err
		}
		for _, t := range raw.Tasks {
			st := t.Status.Status
			if t.Status.Type == "closed" {
				st = "closed"
			}
			out = append(out, extItem{ID: "clickup-" + t.ID, Title: t.Name, URL: t.URL, Status: st})
		}
		if (raw.LastPage != nil && *raw.LastPage) || len(raw.Tasks) < 100 {
			break
		}
	}
	return out, nil
}

// fetchTodoistTasks lists active tasks (API v1, optionally of a project).
// limit ≤ 200, cursor = next_cursor until null. v1 has no url field.
// https://developer.todoist.com/api/v1/
