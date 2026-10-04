package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/httpjson"
	"github.com/aa-blinov/paratrack/internal/httpretry"
	"github.com/aa-blinov/paratrack/internal/integrationport"
)

const maxImportTasks = integrationport.MaxSyncTasks

// Task is a normalized external task returned by a provider API.
type Task struct {
	ID     string
	Title  string
	URL    string
	Status string
}

type extItem = Task

// Config contains process-wide provider defaults. A URL supplied with an
// integration still takes precedence over these defaults.
type Config struct {
	JiraSite   string
	GitLabSite string
}

// Client fetches paginated task snapshots from supported providers.
type Client struct {
	httpClient *http.Client
	retryWaits []time.Duration
	config     Config
}

var ErrIncompleteDependencies = errors.New("integration provider client requires an HTTP client")

// New creates a provider client with bounded default rate-limit retries.
func New(client *http.Client) (*Client, error) {
	return NewConfigured(client, Config{})
}

// NewConfigured creates a provider client with bounded default retries and
// process-wide site defaults supplied by the composition root.
func NewConfigured(client *http.Client, config Config) (*Client, error) {
	return NewWithConfigAndRetryWaits(client, []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}, config)
}

// NewWithRetryWaits allows adapters to supply a shorter retry schedule.
func NewWithRetryWaits(client *http.Client, waits []time.Duration) (*Client, error) {
	return NewWithConfigAndRetryWaits(client, waits, Config{})
}

// NewWithConfigAndRetryWaits allows adapters to supply site defaults and a
// controlled retry schedule.
func NewWithConfigAndRetryWaits(client *http.Client, waits []time.Duration, config Config) (*Client, error) {
	if client == nil {
		return nil, ErrIncompleteDependencies
	}
	config.JiraSite = strings.TrimRight(strings.TrimSpace(config.JiraSite), "/")
	config.GitLabSite = strings.TrimRight(strings.TrimSpace(config.GitLabSite), "/")
	return &Client{httpClient: client, retryWaits: append([]time.Duration(nil), waits...), config: config}, nil
}

// FetchTasks pulls a complete provider snapshot and maps it to application input.
func (c *Client) FetchTasks(ctx context.Context, input integrationport.ProviderInput) ([]integrationport.ProviderTask, error) {
	var tasks []Task
	var err error
	switch input.Provider {
	case "github":
		tasks, err = c.fetchGitHubIssues(ctx, input.Secret, input.Config.Target)
	case "trello":
		tasks, err = c.fetchTrelloCards(ctx, input.Secret, input.Config.Target)
	case "jira":
		tasks, err = c.fetchJiraIssues(ctx, input.Secret, input.Config.Target)
	case "notion":
		tasks, err = c.fetchNotionTasks(ctx, input.Secret, input.Config.Target)
	case "asana":
		tasks, err = c.fetchAsanaTasks(ctx, input.Secret, input.Config.Target)
	case "gitlab":
		tasks, err = c.fetchGitLabIssues(ctx, input.Secret, input.Config.Target)
	case "clickup":
		tasks, err = c.fetchClickUpTasks(ctx, input.Secret, input.Config.Target)
	case "todoist":
		tasks, err = c.fetchTodoistTasks(ctx, input.Secret, input.Config.Target)
	default:
		return nil, fmt.Errorf("unknown provider %q", input.Provider)
	}
	if err != nil {
		return nil, err
	}
	out := make([]integrationport.ProviderTask, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, integrationport.ProviderTask{ExternalID: task.ID, Title: task.Title, URL: task.URL, Status: task.Status})
	}
	return out, nil
}

func (c *Client) getJSON(ctx context.Context, vendor string, mk func() (*http.Request, error), into any) (http.Header, error) {
	resp, err := httpretry.Do(ctx, c.httpClient, c.retryWaits, vendor, mk)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return resp.Header, httpjson.Decode(resp.Body, httpjson.MaxResponseBytes, into)
}

func newReq(method, u, body string, headers ...string) func() (*http.Request, error) {
	return func() (*http.Request, error) {
		var rd io.Reader
		if body != "" {
			rd = strings.NewReader(body)
		}
		req, err := http.NewRequest(method, u, rd)
		if err != nil {
			return nil, err
		}
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		return req, nil
	}
}

var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

var ErrInvalidProviderSite = errors.New("provider site must be an HTTPS origin without credentials or query parameters")

func validateProviderSite(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	site, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(site.Scheme, "https") || site.Hostname() == "" || site.User != nil ||
		site.RawQuery != "" || site.ForceQuery || site.Fragment != "" || site.Opaque != "" {
		return "", ErrInvalidProviderSite
	}
	return raw, nil
}

func linkNext(h http.Header) string {
	if m := linkNextRe.FindStringSubmatch(h.Get("Link")); m != nil {
		return m[1]
	}
	return ""
}

// fetchGitHubIssues lists open issues of "owner/repo", or the token
// owner's assigned issues across repos when repo is empty. Pull requests
// come back from this API too and are skipped. per_page max 100, pages
// via Link rel="next".
// https://docs.github.com/en/rest/issues/issues · …/using-pagination-in-the-rest-api
