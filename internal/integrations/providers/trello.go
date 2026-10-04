package providers

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

func (c *Client) fetchTrelloCards(ctx context.Context, secret, board string) ([]extItem, error) {
	key, token, ok := strings.Cut(secret, ":")
	if !ok || key == "" || token == "" {
		return nil, fmt.Errorf("trello: the secret is key:token")
	}
	const limit = 1000
	var out []extItem
	before := ""
	for len(out) < maxImportTasks {
		u := fmt.Sprintf("https://api.trello.com/1/boards/%s/cards/open?limit=%d&key=%s&token=%s",
			url.PathEscape(board), limit, url.QueryEscape(key), url.QueryEscape(token))
		if before != "" {
			u += "&before=" + url.QueryEscape(before)
		}
		var raw []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			ShortURL string `json:"shortUrl"`
			Closed   bool   `json:"closed"`
		}
		if _, err := c.getJSON(ctx, "trello", newReq("GET", u, ""), &raw); err != nil {
			return nil, err
		}
		for _, c := range raw {
			st := "open"
			if c.Closed {
				st = "closed"
			}
			out = append(out, extItem{ID: "tr-" + c.ID, Title: c.Name, URL: c.ShortURL, Status: st})
		}
		if len(raw) < limit {
			break
		}
		before = raw[len(raw)-1].ID
	}
	return out, nil
}
