package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/integrationport"
	"github.com/aa-blinov/paratrack/internal/providerstatus"
)

// gitHubProjectURL accepts a Projects (v2) board of an organization or a user:
// https://github.com/orgs/<org>/projects/<n> or https://github.com/users/<login>/projects/<n>,
// with an optional view or query after the number.
var gitHubProjectURL = regexp.MustCompile(`^https://github\.com/(orgs|users)/([A-Za-z0-9](?:[A-Za-z0-9-]{0,38}))/projects/([0-9]+)(?:[/?#].*)?$`)

// gitHubProjectQuery reads one page of a board's items. The root field is either
// organization or user, chosen from the URL.
const gitHubProjectQuery = `query($login: String!, $number: Int!, $cursor: String) {
  %s(login: $login) {
    projectV2(number: $number) {
      items(first: 100, after: $cursor) {
        pageInfo { hasNextPage endCursor }
        nodes {
          id
          fieldValueByName(name: "Status") {
            ... on ProjectV2ItemFieldSingleSelectValue { name }
          }
          content {
            __typename
            ... on DraftIssue { title }
            ... on Issue { number title url state repository { nameWithOwner } }
            ... on PullRequest { number title url state repository { nameWithOwner } }
          }
        }
      }
    }
  }
}`

type gitHubProjectResponse struct {
	Data *struct {
		Organization *struct {
			ProjectV2 *gitHubBoard `json:"projectV2"`
		} `json:"organization"`
		User *struct {
			ProjectV2 *gitHubBoard `json:"projectV2"`
		} `json:"user"`
	} `json:"data"`
}

type gitHubBoard struct {
	Items struct {
		PageInfo struct {
			HasNextPage bool   `json:"hasNextPage"`
			EndCursor   string `json:"endCursor"`
		} `json:"pageInfo"`
		Nodes []gitHubItemNode `json:"nodes"`
	} `json:"items"`
}

type gitHubItemNode struct {
	ID    string `json:"id"`
	Field *struct {
		Name string `json:"name"`
	} `json:"fieldValueByName"`
	Content *struct {
		Typename   string `json:"__typename"`
		Number     int    `json:"number"`
		Title      string `json:"title"`
		URL        string `json:"url"`
		State      string `json:"state"`
		Repository *struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
	} `json:"content"`
}

// FetchGitHubProject reads every item of a Projects (v2) board. The token needs
// the project read scope. Items are returned in board order; the caller maps
// their status to columns.
func (c *Client) FetchGitHubProject(ctx context.Context, token, projectURL string) ([]appmodel.GitHubProjectItem, error) {
	match := gitHubProjectURL.FindStringSubmatch(projectURL)
	if match == nil {
		return nil, appmodel.ErrGitHubProjectURL
	}
	number, err := strconv.Atoi(match[3])
	if err != nil || number <= 0 {
		return nil, appmodel.ErrGitHubProjectURL
	}
	root := "organization"
	if match[1] == "users" {
		root = "user"
	}
	login := match[2]

	var out []appmodel.GitHubProjectItem
	cursor := ""
	for {
		board, err := c.gitHubBoardPage(ctx, token, root, login, number, cursor)
		if err != nil {
			return nil, err
		}
		for _, node := range board.Items.Nodes {
			item, ok := gitHubItem(node)
			if !ok {
				continue
			}
			out = append(out, item)
			if len(out) > integrationport.MaxSyncTasks {
				return nil, appmodel.ErrGitHubProjectTooLarge
			}
		}
		if !board.Items.PageInfo.HasNextPage || board.Items.PageInfo.EndCursor == "" {
			return out, nil
		}
		cursor = board.Items.PageInfo.EndCursor
	}
}

type gitHubGraphQLRequest struct {
	Query     string               `json:"query"`
	Variables gitHubBoardVariables `json:"variables"`
}

type gitHubBoardVariables struct {
	Login  string  `json:"login"`
	Number int     `json:"number"`
	Cursor *string `json:"cursor"`
}

func (c *Client) gitHubBoardPage(ctx context.Context, token, root, login string, number int, cursor string) (gitHubBoard, error) {
	request := gitHubGraphQLRequest{
		Query:     fmt.Sprintf(gitHubProjectQuery, root),
		Variables: gitHubBoardVariables{Login: login, Number: number},
	}
	if cursor != "" {
		request.Variables.Cursor = &cursor
	}
	body, err := json.Marshal(request)
	if err != nil {
		return gitHubBoard{}, err
	}
	var raw gitHubProjectResponse
	if _, err := c.getJSON(ctx, "github", newReq("POST", c.config.GitHubGraphQL, string(body),
		"Authorization", "Bearer "+token, "Accept", "application/vnd.github+json",
		"X-GitHub-Api-Version", "2022-11-28", "User-Agent", "paratrack", "Content-Type", "application/json"), &raw); err != nil {
		var status *providerstatus.Error
		if errors.As(err, &status) && (status.StatusCode == http.StatusUnauthorized || status.StatusCode == http.StatusForbidden) {
			return gitHubBoard{}, appmodel.ErrGitHubTokenRejected
		}
		if errors.As(err, &status) && status.StatusCode == http.StatusNotFound {
			return gitHubBoard{}, appmodel.ErrGitHubProjectNotFound
		}
		return gitHubBoard{}, err
	}
	if raw.Data == nil {
		return gitHubBoard{}, appmodel.ErrGitHubProjectNotFound
	}
	var board *gitHubBoard
	switch {
	case raw.Data.Organization != nil && raw.Data.Organization.ProjectV2 != nil:
		board = raw.Data.Organization.ProjectV2
	case raw.Data.User != nil && raw.Data.User.ProjectV2 != nil:
		board = raw.Data.User.ProjectV2
	default:
		return gitHubBoard{}, appmodel.ErrGitHubProjectNotFound
	}
	return *board, nil
}

// gitHubItem maps one board node. Items whose content GitHub does not show are
// skipped: there is nothing to name or link.
func gitHubItem(node gitHubItemNode) (appmodel.GitHubProjectItem, bool) {
	if node.Content == nil {
		return appmodel.GitHubProjectItem{}, false
	}
	item := appmodel.GitHubProjectItem{}
	if node.Field != nil {
		item.Status = node.Field.Name
	}
	content := node.Content
	if content.Typename == "DraftIssue" {
		item.Ref = "github-draft:" + node.ID
		item.Title = content.Title
	} else {
		if content.URL == "" {
			return appmodel.GitHubProjectItem{}, false
		}
		item.Ref = content.URL
		item.URL = content.URL
		item.Title = content.Title
		if content.Repository != nil && content.Repository.NameWithOwner != "" {
			item.Title = fmt.Sprintf("%s#%d %s", content.Repository.NameWithOwner, content.Number, content.Title)
		}
		item.Closed = content.State != "OPEN"
	}
	if item.Title == "" {
		item.Title = item.Ref
	}
	return item, true
}
