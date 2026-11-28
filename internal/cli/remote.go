package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
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

type remoteProjectItem struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type remoteProjectListResponse struct {
	Projects []remoteProjectItem `json:"projects"`
}

type remoteSessionItem struct {
	ID        int64  `json:"id"`
	Activity  string `json:"activity"`
	ProjectID *int64 `json:"project_id"`
	Start     string `json:"start"`
	End       string `json:"end"`
	Seconds   int    `json:"seconds"`
	Note      string `json:"note"`
}

type remoteSessionsResponse struct {
	Sessions []remoteSessionItem `json:"sessions"`
}

type stringListFlag []string

func (s *stringListFlag) String() string { return strings.Join(*s, ",") }
func (s *stringListFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			*s = append(*s, trimmed)
		}
	}
	return nil
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

func (rt *Runtime) resolveRemoteProjectID(projectRef string) (int64, error) {
	ref := strings.TrimSpace(projectRef)
	if ref == "" {
		return 0, nil
	}
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil && id > 0 {
		return id, nil
	}
	_, body, err := rt.remoteDo(http.MethodGet, "/api/projects", nil)
	if err != nil {
		return 0, fmt.Errorf("list projects: %w", err)
	}
	var res remoteProjectListResponse
	if err := json.Unmarshal(body, &res); err != nil {
		return 0, fmt.Errorf("decode projects: %w", err)
	}
	low := strings.ToLower(ref)
	for _, p := range res.Projects {
		if strings.ToLower(p.Slug) == low || strings.ToLower(p.Name) == low {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("project %q not found", projectRef)
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

func runAuth(rt *Runtime, args []string) error {
	if len(args) == 0 || args[0] == "status" {
		if rt.remoteEnabled() {
			fmt.Fprintf(rt.Out, "remote: %s\n", strings.TrimSpace(rt.RemoteURL))
		} else {
			fmt.Fprintln(rt.Out, "remote: not configured")
		}
		if rt.RemoteConfigPath != "" {
			fmt.Fprintf(rt.Out, "config: %s\n", rt.RemoteConfigPath)
		}
		return nil
	}
	switch args[0] {
	case "login":
		fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		urlFlag := fs.String("url", "", "server base URL")
		tokenFlag := fs.String("token", "", "API bearer token")
		if err := fs.Parse(args[1:]); err != nil {
			return &ExitError{Code: 2, Err: err}
		}
		u := strings.TrimRight(strings.TrimSpace(*urlFlag), "/")
		tok := strings.TrimSpace(*tokenFlag)
		if u == "" || tok == "" {
			return &ExitError{Code: 2, Err: errors.New("usage: paratrack auth login --url <url> --token <token>")}
		}
		if strings.TrimSpace(rt.RemoteConfigPath) == "" {
			return errors.New("remote config path is not available")
		}
		if err := os.MkdirAll(filepath.Dir(rt.RemoteConfigPath), 0o700); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}
		payload, err := json.MarshalIndent(struct {
			URL   string `json:"url"`
			Token string `json:"token"`
		}{URL: u, Token: tok}, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(rt.RemoteConfigPath, append(payload, '\n'), 0o600); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		rt.RemoteURL = u
		rt.RemoteToken = tok
		fmt.Fprintf(rt.Out, "✓ saved remote config to %s\n", rt.RemoteConfigPath)
		return nil
	case "logout":
		if strings.TrimSpace(rt.RemoteConfigPath) == "" {
			return nil
		}
		if err := os.Remove(rt.RemoteConfigPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove config: %w", err)
		}
		rt.RemoteURL = ""
		rt.RemoteToken = ""
		fmt.Fprintln(rt.Out, "✓ removed saved remote config")
		return nil
	default:
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack auth <login|status|logout>")}
	}
}

func parseFlagsAnyOrder(fs *flag.FlagSet, args []string) error {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			if !strings.Contains(a, "=") {
				name := strings.TrimLeft(a, "-")
				if f := fs.Lookup(name); f != nil {
					if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); !ok || !bf.IsBoolFlag() {
						if i+1 < len(args) {
							i++
							flags = append(flags, args[i])
						}
					}
				}
			}
			continue
		}
		pos = append(pos, a)
	}
	return fs.Parse(append(flags, pos...))
}

func runRemote(rt *Runtime, cmd string, args []string) (handled bool, err error) {
	switch cmd {
	case "status", "st":
		fs := flag.NewFlagSet("status", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		shortFlag := fs.Bool("short", false, "compact one-line status for status bars")
		jsonFlag := fs.Bool("json", false, "output JSON")
		if err := parseFlagsAnyOrder(fs, args); err != nil {
			return true, &ExitError{Code: 2, Err: err}
		}
		items, err := rt.remoteActive()
		if err != nil {
			return true, err
		}
		if *jsonFlag {
			enc := json.NewEncoder(rt.Out)
			enc.SetIndent("", "  ")
			return true, enc.Encode(remoteActiveResponse{Items: items})
		}
		if *shortFlag {
			if len(items) == 0 {
				fmt.Fprintln(rt.Out, "idle")
				return true, nil
			}
			parts := make([]string, 0, len(items))
			for _, item := range items {
				label := item.ActivityName + " " + item.Duration
				if item.Paused {
					label = item.ActivityName + " (paused) " + item.Duration
				}
				parts = append(parts, label)
			}
			fmt.Fprintln(rt.Out, strings.Join(parts, " | "))
			return true, nil
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
		project := fs.String("project", "", "project name, slug, or id")
		var tags stringListFlag
		fs.Var(&tags, "tag", "tag to attach (repeatable or comma-separated)")
		if err := parseFlagsAnyOrder(fs, args); err != nil {
			return true, &ExitError{Code: 2, Err: err}
		}
		if fs.NArg() < 1 {
			return true, &ExitError{Code: 2, Err: errors.New("usage: paratrack start <activity> [--project ...] [--tag ...] [--note ...]")}
		}
		name := fs.Arg(0)
		form := url.Values{"activity": {name}}
		if *note != "" {
			form.Set("note", *note)
		}
		if *project != "" {
			pid, err := rt.resolveRemoteProjectID(*project)
			if err != nil {
				return true, err
			}
			form.Set("project_id", strconv.FormatInt(pid, 10))
		}
		if _, _, err := rt.remoteDo(http.MethodPost, "/api/start", form); err != nil {
			return true, fmt.Errorf("start activity: %w", err)
		}
		if len(tags) > 0 {
			items, err := rt.remoteActive()
			if err != nil {
				return true, fmt.Errorf("started %q, but failed to load active session for tags: %w", name, err)
			}
			if item, findErr := findRemoteSession(items, name); findErr == nil {
				for _, tag := range tags {
					if _, _, tagErr := rt.remoteDo(http.MethodPost, fmt.Sprintf("/api/sessions/%d/tags", item.ID), url.Values{"name": {tag}}); tagErr != nil {
						return true, fmt.Errorf("attach tag %q: %w", tag, tagErr)
					}
				}
			}
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
		target := args[0]
		focusTarget := target
		if _, err := strconv.ParseInt(target, 10, 64); err == nil {
			items, err := rt.remoteActive()
			if err != nil {
				return true, err
			}
			item, err := findRemoteSession(items, target)
			if err != nil {
				return true, err
			}
			// The server focuses by activity name; a session id in the path
			// would be read as an activity called "7" and answer 404.
			focusTarget = item.ActivityName
		}
		if _, _, err := rt.remoteDo(http.MethodPost, "/api/focus/"+url.PathEscape(focusTarget), nil); err != nil {
			return true, fmt.Errorf("focus session: %w", err)
		}
		fmt.Fprintf(rt.Out, "✓ focused on %q\n", focusTarget)
		return true, nil

	case "add", "a":
		fs := flag.NewFlagSet("add", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		activityFlag := fs.String("activity", "", "activity name")
		startFlag := fs.String("start", "", "start time (e.g. '2026-11-28 10:00' or '2 hours ago')")
		durationFlag := fs.String("duration", "", "duration (e.g. '1h 30m')")
		endFlag := fs.String("end", "", "end time")
		projectFlag := fs.String("project", "", "project name, slug, or id")
		noteFlag := fs.String("note", "", "session note")
		if err := fs.Parse(args); err != nil {
			return true, &ExitError{Code: 2, Err: err}
		}
		name := strings.TrimSpace(*activityFlag)
		if name == "" && fs.NArg() > 0 {
			name = strings.TrimSpace(fs.Arg(0))
		}
		if name == "" || strings.TrimSpace(*startFlag) == "" || (strings.TrimSpace(*durationFlag) == "" && strings.TrimSpace(*endFlag) == "") {
			return true, &ExitError{Code: 2, Err: errors.New("usage: paratrack add --activity <name> --start <time> (--duration <dur> | --end <time>) [--project ...] [--note ...]")}
		}
		now := rt.now()
		startAt, endAt, err := resolveAddInterval(rt, *startFlag, "", *durationFlag, *endFlag, now)
		if err != nil {
			return true, err
		}
		form := url.Values{
			"activity":     {name},
			"start":        {startAt.In(now.Location()).Format("2006-01-02 15:04")},
			"end":          {endAt.In(now.Location()).Format("2006-01-02 15:04")},
			"confirm_long": {"1"},
		}
		if *noteFlag != "" {
			form.Set("note", *noteFlag)
		}
		if *projectFlag != "" {
			pid, err := rt.resolveRemoteProjectID(*projectFlag)
			if err != nil {
				return true, err
			}
			form.Set("project_id", strconv.FormatInt(pid, 10))
		}
		if _, _, err := rt.remoteDo(http.MethodPost, "/api/sessions/backfill", form); err != nil {
			return true, fmt.Errorf("add session: %w", err)
		}
		fmt.Fprintf(rt.Out, "✓ added %q %s → %s\n", name, startAt.Format("2006-01-02 15:04"), endAt.Format("2006-01-02 15:04"))
		return true, nil

	case "log", "l":
		fs := flag.NewFlagSet("log", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		periodFlag := fs.String("period", "today", "today|yesterday|week|last_week|month|last_month|custom")
		activityFlag := fs.String("activity", "", "filter by activity name")
		startFlag := fs.String("start", "", "start NL (for custom period)")
		endFlag := fs.String("end", "", "end NL (for custom period)")
		jsonFlag := fs.Bool("json", false, "output JSON")
		if err := fs.Parse(args); err != nil {
			return true, &ExitError{Code: 2, Err: err}
		}
		now := rt.now()
		period, err := resolvePeriodCLI(rt, fs, *periodFlag, *startFlag, *endFlag, now)
		if err != nil {
			return true, err
		}
		q := url.Values{
			"from":  {period.Start.Format("2006-01-02")},
			"to":    {period.End.Add(-time.Nanosecond).Format("2006-01-02")},
			"limit": {"200"},
		}
		_, body, err := rt.remoteDo(http.MethodGet, "/api/v1/sessions?"+q.Encode(), nil)
		if err != nil {
			return true, err
		}
		var res remoteSessionsResponse
		if err := json.Unmarshal(body, &res); err != nil {
			return true, fmt.Errorf("decode sessions: %w", err)
		}
		filter := strings.ToLower(strings.TrimSpace(*activityFlag))
		filtered := make([]remoteSessionItem, 0, len(res.Sessions))
		for _, item := range res.Sessions {
			if filter != "" && strings.ToLower(item.Activity) != filter {
				continue
			}
			filtered = append(filtered, item)
		}
		if *jsonFlag {
			enc := json.NewEncoder(rt.Out)
			enc.SetIndent("", "  ")
			return true, enc.Encode(remoteSessionsResponse{Sessions: filtered})
		}
		if len(filtered) == 0 {
			fmt.Fprintf(rt.Out, "no sessions in %s\n", period.Label)
			return true, nil
		}
		tw := tabwriter.NewWriter(rt.Out, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "ACTIVITY\tSTART\tEND\tDURATION\tNOTE\n")
		total := 0
		for _, item := range filtered {
			total += item.Seconds
			startLabel := item.Start
			if t, err := time.Parse(time.RFC3339, item.Start); err == nil {
				startLabel = t.In(now.Location()).Format("01-02 15:04")
			}
			endLabel := "running"
			if item.End != "" {
				if t, err := time.Parse(time.RFC3339, item.End); err == nil {
					endLabel = t.In(now.Location()).Format("01-02 15:04")
				} else {
					endLabel = item.End
				}
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", item.Activity, startLabel, endLabel, formatCLIDuration(item.Seconds), item.Note)
		}
		fmt.Fprintf(tw, "\t\t\t%s\t\n", formatCLIDuration(total))
		_ = tw.Flush()
		return true, nil

	case "stats":
		fs := flag.NewFlagSet("stats", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		periodFlag := fs.String("period", "", "today|yesterday|week|last_week|month|last_month")
		jsonFlag := fs.Bool("json", false, "output JSON")
		if err := fs.Parse(args); err != nil {
			return true, &ExitError{Code: 2, Err: err}
		}
		period := strings.TrimSpace(*periodFlag)
		if period == "" && fs.NArg() > 0 {
			period = fs.Arg(0)
		}
		if period == "" {
			period = "today"
		}
		_, body, err := rt.remoteDo(http.MethodGet, "/api/v1/reports/summary?period="+url.QueryEscape(period), nil)
		if err != nil {
			return true, err
		}
		var rep remoteReportResponse
		if err := json.Unmarshal(body, &rep); err != nil {
			return true, fmt.Errorf("decode summary: %w", err)
		}
		if *jsonFlag {
			enc := json.NewEncoder(rt.Out)
			enc.SetIndent("", "  ")
			return true, enc.Encode(rep)
		}
		fmt.Fprintf(rt.Out, "Total (%s .. %s): %s\n", rep.From, rep.To, formatCLIDuration(rep.TotalSeconds))
		type row struct {
			name string
			secs int
		}
		rows := make([]row, 0, len(rep.ByActivity))
		for act, secs := range rep.ByActivity {
			rows = append(rows, row{name: act, secs: secs})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].secs != rows[j].secs {
				return rows[i].secs > rows[j].secs
			}
			return rows[i].name < rows[j].name
		})
		for _, r := range rows {
			fmt.Fprintf(rt.Out, "  %-20s %s\n", r.name, formatCLIDuration(r.secs))
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
