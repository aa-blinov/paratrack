package providers

import (
	"context"
	"fmt"
	"net/http"
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
		u := fmt.Sprintf("https://api.trello.com/1/boards/%s/cards/open?limit=%d",
			url.PathEscape(board), limit)
		if before != "" {
			u += "&before=" + url.QueryEscape(before)
		}
		var raw []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			ShortURL string `json:"shortUrl"`
			Closed   bool   `json:"closed"`
		}
		// The key and token ride in Basic auth rather than the query string:
		// a transport failure logs the URL, and the text of a *url.Error
		// would carry both to whoever reads the log.
		build := newReq("GET", u, "")
		auth := func() (*http.Request, error) {
			req, err := build()
			if err == nil {
				req.SetBasicAuth(key, token)
			}
			return req, err
		}
		if _, err := c.getJSON(ctx, "trello", auth, &raw); err != nil {
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
