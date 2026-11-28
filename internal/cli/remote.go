package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type remoteActiveItem struct {
	ID           int64  `json:"ID"`
	ActivityName string `json:"ActivityName"`
	ProjectName  string `json:"ProjectName"`
	Duration     string `json:"Duration"`
	Paused       bool   `json:"Paused"`
	Note         string `json:"Note"`
}

type remoteActiveResponse struct {
	Items []remoteActiveItem `json:"Items"`
}

type remoteReportResponse struct {
	From         string         `json:"from"`
	To           string         `json:"to"`
	TotalSeconds int            `json:"total_seconds"`
	ByActivity   map[string]int `json:"by_activity"`
}

func (rt *Runtime) remoteEnabled() bool {
	return strings.TrimSpace(rt.RemoteURL) != "" && strings.TrimSpace(rt.RemoteToken) != ""
}

func (rt *Runtime) httpClient() (*http.Client, error) {
	if rt.HTTPClient == nil {
		return nil, errors.New("remote HTTP client is not configured")
	}
	return rt.HTTPClient, nil
}

func (rt *Runtime) remoteDo(method, path string, form url.Values) (int, []byte, error) {
	client, err := rt.httpClient()
	if err != nil {
		return 0, nil, err
	}
	base := strings.TrimRight(strings.TrimSpace(rt.RemoteURL), "/")
	var bodyReader io.Reader
	if form != nil {
		bodyReader = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(rt.Context, method, base+path, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(rt.RemoteToken))
	req.Header.Set("Accept", "application/json")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode >= 400 {
		var errBody struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errBody) == nil && errBody.Error != "" {
			return resp.StatusCode, body, errors.New(errBody.Error)
		}
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return resp.StatusCode, body, errors.New(msg)
	}
	return resp.StatusCode, body, nil
}

func (rt *Runtime) remoteActive() ([]remoteActiveItem, error) {
	_, body, err := rt.remoteDo(http.MethodGet, "/api/active", nil)
	if err != nil {
		return nil, err
	}
	var res remoteActiveResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("decode active sessions: %w", err)
	}
	return res.Items, nil
}

func findRemoteSession(items []remoteActiveItem, target string) (remoteActiveItem, error) {
	if id, err := strconv.ParseInt(target, 10, 64); err == nil {
		for _, item := range items {
			if item.ID == id {
				return item, nil
			}
		}
		return remoteActiveItem{}, fmt.Errorf("no active session #%d", id)
	}
	low := strings.ToLower(strings.TrimSpace(target))
	for _, item := range items {
		if strings.ToLower(item.ActivityName) == low {
			return item, nil
		}
	}
	return remoteActiveItem{}, fmt.Errorf("no active session matching %q", target)
}

func runRemote(rt *Runtime, cmd string, args []string) (handled bool, err error) {
	switch cmd {
	case "status", "st":
		items, err := rt.remoteActive()
		if err != nil {
			return true, err
		}
		if len(items) == 0 {
			fmt.Fprintln(rt.Out, "no active timers")
			return true, nil
		}
		for _, item := range items {
			state := "running"
			if item.Paused {
				state = "paused"
			}
			extra := ""
			if item.ProjectName != "" {
				extra += " [" + item.ProjectName + "]"
			}
			if item.Note != "" {
				extra += " — " + item.Note
			}
			fmt.Fprintf(rt.Out, "  #%d  %-18s  %8s  (%s)%s\n", item.ID, item.ActivityName, item.Duration, state, extra)
		}
		return true, nil

	case "start":
		fs := flag.NewFlagSet("start", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		note := fs.String("note", "", "optional note for the session")
		if err := fs.Parse(args); err != nil {
			return true, &ExitError{Code: 2, Err: err}
		}
		if fs.NArg() < 1 {
			return true, &ExitError{Code: 2, Err: errors.New("usage: paratrack start <activity> [--note ...]")}
		}
		name := fs.Arg(0)
		form := url.Values{"activity": {name}}
		if *note != "" {
			form.Set("note", *note)
		}
		if _, _, err := rt.remoteDo(http.MethodPost, "/api/start", form); err != nil {
			return true, fmt.Errorf("start activity: %w", err)
		}
		fmt.Fprintf(rt.Out, "✓ started %q\n", name)
		return true, nil

	case "stop", "s":
		target := ""
		if len(args) > 0 {
			target = args[0]
		}
		if target == "" {
			if _, _, err := rt.remoteDo(http.MethodPost, "/api/active/stop-all", nil); err != nil {
				return true, fmt.Errorf("stop all: %w", err)
			}
			fmt.Fprintln(rt.Out, "✓ stopped active timers")
			return true, nil
		}
		items, err := rt.remoteActive()
		if err != nil {
			return true, err
		}
		item, err := findRemoteSession(items, target)
		if err != nil {
			return true, err
		}
		if _, _, err := rt.remoteDo(http.MethodPost, fmt.Sprintf("/api/sessions/%d/stop", item.ID), nil); err != nil {
			return true, fmt.Errorf("stop session: %w", err)
		}
		fmt.Fprintf(rt.Out, "✓ stopped %q (#%d)\n", item.ActivityName, item.ID)
		return true, nil

	case "pause", "p":
		target := ""
		if len(args) > 0 {
			target = args[0]
		}
		if target == "" {
			if _, _, err := rt.remoteDo(http.MethodPost, "/api/active/pause-all", nil); err != nil {
				return true, fmt.Errorf("pause all: %w", err)
			}
			fmt.Fprintln(rt.Out, "✓ paused active timers")
			return true, nil
		}
		items, err := rt.remoteActive()
		if err != nil {
			return true, err
		}
		item, err := findRemoteSession(items, target)
		if err != nil {
			return true, err
		}
		if _, _, err := rt.remoteDo(http.MethodPost, fmt.Sprintf("/api/sessions/%d/pause", item.ID), nil); err != nil {
			return true, fmt.Errorf("pause session: %w", err)
		}
		fmt.Fprintf(rt.Out, "✓ paused %q (#%d)\n", item.ActivityName, item.ID)
		return true, nil

	case "resume", "r":
		if len(args) < 1 {
			return true, &ExitError{Code: 2, Err: errors.New("usage: paratrack resume <activity|id>")}
		}
		items, err := rt.remoteActive()
		if err != nil {
			return true, err
		}
		item, err := findRemoteSession(items, args[0])
		if err != nil {
			return true, err
		}
		if _, _, err := rt.remoteDo(http.MethodPost, fmt.Sprintf("/api/sessions/%d/resume", item.ID), nil); err != nil {
			return true, fmt.Errorf("resume session: %w", err)
		}
		fmt.Fprintf(rt.Out, "✓ resumed %q (#%d)\n", item.ActivityName, item.ID)
		return true, nil

	case "focus", "switch", "sw":
		if len(args) < 1 {
			return true, &ExitError{Code: 2, Err: errors.New("usage: paratrack focus <activity|id>")}
		}
		items, err := rt.remoteActive()
		if err != nil {
			return true, err
		}
		item, err := findRemoteSession(items, args[0])
		if err != nil {
			return true, err
		}
		if _, _, err := rt.remoteDo(http.MethodPost, fmt.Sprintf("/api/focus/%d", item.ID), nil); err != nil {
			return true, fmt.Errorf("focus session: %w", err)
		}
		fmt.Fprintf(rt.Out, "✓ focused on %q (#%d)\n", item.ActivityName, item.ID)
		return true, nil

	case "stats":
		period := "today"
		if len(args) > 0 {
			period = args[0]
		}
		_, body, err := rt.remoteDo(http.MethodGet, "/api/v1/reports/summary?period="+url.QueryEscape(period), nil)
		if err != nil {
			return true, err
		}
		var rep remoteReportResponse
		if err := json.Unmarshal(body, &rep); err != nil {
			return true, fmt.Errorf("decode summary: %w", err)
		}
		fmt.Fprintf(rt.Out, "Total (%s .. %s): %s\n", rep.From, rep.To, formatCLIDuration(rep.TotalSeconds))
		for act, secs := range rep.ByActivity {
			fmt.Fprintf(rt.Out, "  %-20s %s\n", act, formatCLIDuration(secs))
		}
		return true, nil

	default:
		return false, nil
	}
}

func formatCLIDuration(secs int) string {
	h := secs / 3600
	m := (secs % 3600) / 60
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
