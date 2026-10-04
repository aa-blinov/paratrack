package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// NotionVersion is the data-sources API: a database can hold several data
// sources, and the old databases/{id}/query fails on those.
// https://developers.notion.com/docs/upgrade-guide-2025-09-03 · …/reference/pagination
const NotionVersion = "2025-09-03"

type notionQueryRequest struct {
	PageSize    int    `json:"page_size"`
	StartCursor string `json:"start_cursor,omitempty"`
}

// fetchNotionTasks reads every page of every data source of a database.
// secret = internal integration secret; target = database id (32 hex).
// No filter: a database needn't have a "Status" property.
func (c *Client) fetchNotionTasks(ctx context.Context, secret, dbID string) ([]extItem, error) {
	if dbID == "" {
		return nil, fmt.Errorf("notion database id is required")
	}
	dbID = strings.ReplaceAll(dbID, "-", "")
	if len(dbID) != 32 {
		return nil, fmt.Errorf("notion database id must be 32 hex chars")
	}
	formatted := dbID[0:8] + "-" + dbID[8:12] + "-" + dbID[12:16] + "-" + dbID[16:20] + "-" + dbID[20:32]
	hdr := []string{"Authorization", "Bearer " + secret, "Notion-Version", NotionVersion, "Content-Type", "application/json"}
	var db struct {
		DataSources []struct {
			ID string `json:"id"`
		} `json:"data_sources"`
	}
	if _, err := c.getJSON(ctx, "notion", newReq("GET", "https://api.notion.com/v1/databases/"+formatted, "", hdr...), &db); err != nil {
		return nil, err
	}
	var out []extItem
	for _, src := range db.DataSources {
		cursor := ""
		for len(out) < maxImportTasks {
			body, err := json.Marshal(notionQueryRequest{PageSize: 100, StartCursor: cursor})
			if err != nil {
				return nil, fmt.Errorf("encode Notion query: %w", err)
			}
			var raw struct {
				Results []struct {
					ID         string `json:"id"`
					URL        string `json:"url"`
					InTrash    bool   `json:"in_trash"`
					Properties map[string]struct {
						Title []struct {
							PlainText string `json:"plain_text"`
						} `json:"title"`
					} `json:"properties"`
				} `json:"results"`
				HasMore    bool   `json:"has_more"`
				NextCursor string `json:"next_cursor"`
			}
			if _, err := c.getJSON(ctx, "notion", newReq("POST", "https://api.notion.com/v1/data_sources/"+src.ID+"/query", string(body), hdr...), &raw); err != nil {
				return nil, err
			}
			for _, r := range raw.Results {
				if r.InTrash {
					continue
				}
				title := "Untitled"
				for _, v := range r.Properties {
					if len(v.Title) > 0 && v.Title[0].PlainText != "" {
						title = v.Title[0].PlainText
						break
					}
				}
				out = append(out, extItem{ID: "notion-" + r.ID, Title: title, URL: r.URL, Status: "open"})
			}
			if !raw.HasMore || raw.NextCursor == "" {
				break
			}
			cursor = raw.NextCursor
		}
	}
	return out, nil
}

// fetchAsanaTasks lists incomplete tasks of a project (completed_since=now).
// limit ≤ 100; next page via next_page.offset, until next_page is null.
// https://developers.asana.com/reference/gettasksforproject · …/docs/pagination
