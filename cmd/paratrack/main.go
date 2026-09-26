// Command paratrack is a minimalist time tracker (CLI + embedded web UI).
//
// Usage:
//
//	paratrack status                 # show active sessions
//	paratrack start <activity>       # quick start
//	paratrack stop [activity]        # stop specific or all active
//	paratrack web [--addr :8000]     # launch embedded web UI
//	paratrack migrate-to-postgres <track.db>  # copy SQLite into PARATRACK_DATABASE_URL
//
// Commands read/write ~/.track/track.db, or Postgres when
// PARATRACK_DATABASE_URL is set.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/aa-blinov/paratrack/internal/cli"
	"github.com/aa-blinov/paratrack/internal/db"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
	"github.com/aa-blinov/paratrack/internal/web"

	"os/exec"
)

func main() {
	if len(os.Args) < 2 {
		runStatus() // default: show what's running
		return
	}
	switch os.Args[1] {
	case "status", "st":
		runStatus()
	case "start":
		runStart(os.Args[2:])
	case "stop", "s":
		runStop(os.Args[2:])
	case "pause", "p":
		runPause(os.Args[2:])
	case "resume", "r":
		runResume(os.Args[2:])
	case "focus", "switch", "sw":
		runFocus(os.Args[2:])
	case "add", "a":
		runAdd(os.Args[2:])
	case "log", "l":
		runLog(os.Args[2:])
	case "stats":
		runStats(os.Args[2:])
	case "goal", "goals":
		runGoal(os.Args[2:])
	case "tag", "tags":
		runTag(os.Args[2:])
	case "project", "projects":
		runProject(os.Args[2:])
	case "web":
		runWeb(os.Args[2:])
	case "migrate-to-postgres":
		runMigrateToPostgres(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Println(`paratrack — minimalist time tracker

Usage:
  paratrack                         show active sessions
  paratrack start <activity>        quick start (asks for note)
  paratrack stop [activity]         stop specific or selected sessions
  paratrack pause [activity]        pause active session(s)
  paratrack resume [activity]       resume paused session(s)
  paratrack focus <activity>        pause others, resume/start selected
  paratrack add                     add a past session (interactive)
  paratrack log                     show log for a period
  paratrack stats                   show aggregated stats
  paratrack web [--addr :8000]      launch embedded web UI

Aliases: s=stop, p=pause, r=resume, sw=switch, st=status, a=add, l=log
  paratrack goal set --activity <name> --daily 2h   set a target
  paratrack goal list                               show goals + progress
  paratrack goal unset --activity <name> [--daily]  remove
  paratrack tag add <name>                          create a tag
  paratrack tag list                                show all tags + counts
  paratrack tag attach <session_id> <name>          tag a session
  paratrack tag detach <session_id> <name>          untag
  paratrack project list [--archived] [--team id]   list projects
  paratrack project create [--slug s] [--color #hex] [--team id] <name>  create
  paratrack project show <slug|id>                  show one project
  paratrack project rename <slug|id> <new-name>     rename
  paratrack project color   <slug|id> <#hex>        set color
  paratrack project archive <slug|id>               archive
  paratrack project unarchive <slug|id>             unarchive
  paratrack project delete <slug|id>                delete`)
}

// --- helpers ---------------------------------------------------------

// openDB opens Postgres when PARATRACK_DATABASE_URL is set, otherwise the
// SQLite file at the default path; exits with a friendly message on error.
func openDB() (*db.DB, context.Context) {
	d, err := db.OpenDefault()
	if err != nil {
		fatal("open database: %v", err)
	}
	return d, context.Background()
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "paratrack: "+format+"\n", args...)
	os.Exit(1)
}

func shortDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, (total/60)%60, total%60)
}

// printActive renders the active-session table for both `status` and
// the bare `paratrack` invocation.
func printActive(rows []model.ActiveSession) {
	if len(rows) == 0 {
		fmt.Println("No active sessions. Start one with: paratrack start <activity>")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ACTIVITY\tSTATUS\tSTARTED\tDURATION")
	fmt.Fprintln(tw, "--------\t------\t-------\t--------")
	now := time.Now()
	for _, as := range rows {
		status := "Active"
		if as.Session.Paused {
			status = "Paused"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			as.Activity.Name,
			status,
			as.Session.StartAt.Local().Format("15:04:05"),
			shortDur(time.Duration(as.Session.DurationSeconds(now))*time.Second),
		)
	}
	_ = tw.Flush()
}

// --- commands --------------------------------------------------------

func runStatus() {
	d, ctx := openDB()
	defer d.Close()
	rows, err := d.ListActiveSessions(ctx, 0)
	if err != nil {
		fatal("list active: %v", err)
	}
	printActive(rows)
}

func runStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	note := fs.String("note", "", "optional note for the session")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: paratrack start <activity> [--note ...]")
		os.Exit(2)
	}
	name := fs.Arg(0)
	d, ctx := openDB()
	defer d.Close()

	act, err := d.GetOrCreateActivity(ctx, 0, name)
	if err != nil {
		fatal("get-or-create activity: %v", err)
	}
	// Reject if there's already an open session for this activity.
	active, err := d.ListActiveSessions(ctx, 0)
	if err != nil {
		fatal("list active: %v", err)
	}
	for _, as := range active {
		if as.Session.ActivityID == act.ID {
			fmt.Fprintf(os.Stderr, "activity %q already has an active session (id %d) — use stop or pause first\n", act.Name, as.Session.ID)
			os.Exit(1)
		}
	}
	s, err := d.CreateSession(ctx, 0, act.ID, time.Now(), *note)
	if err != nil {
		fatal("create session: %v", err)
	}
	fmt.Printf("✓ started %q (session #%d)\n", act.Name, s.ID)
	if *note != "" {
		fmt.Printf("  note: %s\n", *note)
	}
}

func runStop(args []string) {
	d, ctx := openDB()
	defer d.Close()
	active, err := d.ListActiveSessions(ctx, 0)
	if err != nil {
		fatal("list active: %v", err)
	}
	if len(active) == 0 {
		fmt.Println("No active sessions to stop.")
		return
	}
	fs := flag.NewFlagSet("stop", flag.ExitOnError)
	_ = fs.Parse(args)
	target := ""
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}
	stopped := 0
	for _, as := range active {
		if target == "" || equalsFold(as.Activity.Name, target) {
			if _, err := d.UpdateSessionEnd(ctx, 0, as.Session.ID, time.Now()); err != nil {
				fatal("stop %d: %v", as.Session.ID, err)
			}
			fmt.Printf("✓ stopped %q\n", as.Activity.Name)
			stopped++
		}
	}
	if stopped == 0 && target != "" {
		fmt.Fprintf(os.Stderr, "no active session for %q\n", target)
		os.Exit(1)
	}
}

func runPause(args []string) {
	d, ctx := openDB()
	defer d.Close()
	fs := flag.NewFlagSet("pause", flag.ExitOnError)
	_ = fs.Parse(args)
	target := ""
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}
	now := time.Now()
	count := 0
	active, err := d.ListActiveSessions(ctx, 0)
	if err != nil {
		fatal("list active: %v", err)
	}
	for _, as := range active {
		if as.Session.Paused {
			continue
		}
		if target != "" && !equalsFold(as.Activity.Name, target) {
			continue
		}
		if _, err := d.PauseSession(ctx, 0, as.Session.ID, now); err != nil {
			fatal("pause %d: %v", as.Session.ID, err)
		}
		count++
	}
	if count == 0 {
		fmt.Println("Nothing to pause.")
		return
	}
	fmt.Printf("✓ paused %d session(s)\n", count)
}

func runResume(args []string) {
	d, ctx := openDB()
	defer d.Close()
	fs := flag.NewFlagSet("resume", flag.ExitOnError)
	_ = fs.Parse(args)
	target := ""
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}
	now := time.Now()
	count := 0
	active, err := d.ListActiveSessions(ctx, 0)
	if err != nil {
		fatal("list active: %v", err)
	}
	for _, as := range active {
		if !as.Session.Paused {
			continue
		}
		if target != "" && !equalsFold(as.Activity.Name, target) {
			continue
		}
		if _, err := d.ResumeSession(ctx, 0, as.Session.ID, now); err != nil {
			fatal("resume %d: %v", as.Session.ID, err)
		}
		count++
	}
	if count == 0 {
		fmt.Println("Nothing to resume.")
		return
	}
	fmt.Printf("✓ resumed %d session(s)\n", count)
}

func runFocus(args []string) {
	fs := flag.NewFlagSet("focus", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: paratrack focus <activity>")
		os.Exit(2)
	}
	target := fs.Arg(0)
	d, ctx := openDB()
	defer d.Close()
	act, err := d.FindActivityByName(ctx, 0, target)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		fatal("find activity: %v", err)
	}
	if errors.Is(err, db.ErrNotFound) {
		// Auto-create on focus (matches Python behaviour).
		act, err = d.CreateActivity(ctx, 0, target)
		if err != nil {
			fatal("create activity: %v", err)
		}
	}
	active, err := d.ListActiveSessions(ctx, 0)
	if err != nil {
		fatal("list active: %v", err)
	}
	paused, resumed := 0, 0
	targetExists := false
	now := time.Now()
	for _, as := range active {
		if as.Activity.ID == act.ID {
			targetExists = true
			if as.Session.Paused {
				if _, err := d.ResumeSession(ctx, 0, as.Session.ID, now); err != nil {
					fatal("resume: %v", err)
				}
				resumed++
			}
			continue
		}
		if !as.Session.Paused {
			if _, err := d.PauseSession(ctx, 0, as.Session.ID, now); err != nil {
				fatal("pause: %v", err)
			}
			paused++
		}
	}
	if !targetExists {
		if _, err := d.CreateSession(ctx, 0, act.ID, now, ""); err != nil {
			fatal("start: %v", err)
		}
		fmt.Printf("✓ focus started on %q (paused %d other)\n", act.Name, paused)
		return
	}
	fmt.Printf("✓ focus on %q (resumed %d, paused %d other)\n", act.Name, resumed, paused)
}

func runAdd(args []string) {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	activityFlag := fs.String("activity", "", "activity name (skip interactive picker)")
	startFlag := fs.String("start", "", "start time NL (e.g. '2 hours ago')")
	modeFlag := fs.String("mode", "", "duration | end (skip prompt)")
	durationFlag := fs.String("duration", "", "duration NL (e.g. '1h 30m')")
	endFlag := fs.String("end", "", "end time NL")
	noteFlag := fs.String("note", "", "session note")
	_ = fs.Parse(args)

	d, ctx := openDB()
	defer d.Close()
	now := time.Now()

	// 1) Activity
	var act model.Activity
	if *activityFlag != "" {
		a, err := d.GetOrCreateActivity(ctx, 0, *activityFlag)
		if err != nil {
			fatal("get-or-create: %v", err)
		}
		act = a
	} else {
		a, err := pickActivity(ctx, d, "activity (or new):", true)
		if err != nil {
			if errors.Is(err, cli.ErrCancelled) {
				fmt.Println("cancelled")
				return
			}
			fatal("%v", err)
		}
		if a == nil {
			fmt.Println("cancelled")
			return
		}
		act = *a
	}

	// 2) Start time
	startStr := *startFlag
	if startStr == "" {
		s, err := cli.Prompt(cli.Stdin, cli.Stderr, "start (e.g. '2 hours ago', 'yesterday 14:00')", "2 hours ago")
		if err != nil {
			fatal("read start: %v", err)
		}
		startStr = s
	}
	startAt, err := timeparse.ParseDateTime(startStr, now)
	if err != nil {
		fatal("parse start %q: %v", startStr, err)
	}

	// 3) Mode (duration vs end)
	mode := *modeFlag
	if mode == "" {
		idx, err := cli.Choose(cli.Stdin, cli.Stderr, "provide duration or end time?",
			[]string{"duration (e.g. 1h, 90m)", "end time (e.g. 'yesterday 16:30')"}, 0)
		if err != nil {
			fatal("read mode: %v", err)
		}
		if idx == 0 {
			mode = "duration"
		} else {
			mode = "end"
		}
	}

	// 4) Compute end
	var endAt time.Time
	switch mode {
	case "duration":
		durStr := *durationFlag
		if durStr == "" {
			s, err := cli.Prompt(cli.Stdin, cli.Stderr, "duration", "1h")
			if err != nil {
				fatal("read duration: %v", err)
			}
			durStr = s
		}
		secs, err := timeparse.ParseDuration(durStr)
		if err != nil {
			fatal("parse duration %q: %v", durStr, err)
		}
		endAt = startAt.Add(time.Duration(secs) * time.Second)
	case "end":
		endStr := *endFlag
		if endStr == "" {
			s, err := cli.Prompt(cli.Stdin, cli.Stderr, "end time (e.g. 'yesterday 16:30')", "")
			if err != nil {
				fatal("read end: %v", err)
			}
			endStr = s
		}
		t, err := timeparse.ParseDateTime(endStr, now)
		if err != nil {
			fatal("parse end %q: %v", endStr, err)
		}
		if !t.After(startAt) {
			fatal("end time must be after start time")
		}
		endAt = t
	default:
		fatal("unknown mode %q (use 'duration' or 'end')", mode)
	}

	// 5) Note
	note := *noteFlag
	if !flagWasSet(fs, "note") {
		if s, err := cli.Prompt(cli.Stdin, cli.Stderr, "note (optional)", ""); err == nil {
			note = s
		}
	}

	// 6) Create closed session
	_, err = d.CreateClosedSession(ctx, 0, act.ID, startAt, endAt, note)
	if err != nil {
		fatal("create session: %v", err)
	}
	fmt.Printf("✓ added %q %s → %s\n", act.Name,
		startAt.Format("2006-01-02 15:04"), endAt.Format("2006-01-02 15:04"))
}

// pickActivity shows a numbered list of known activities and asks the user
// to pick one. When allowNew is true, the last option is "+ new".
// Returns (nil, ErrCancelled) if the user types ".".
func pickActivity(ctx context.Context, d *db.DB, label string, allowNew bool) (*model.Activity, error) {
	acts, err := d.ListActivities(ctx, 0, false)
	if err != nil {
		return nil, err
	}
	options := make([]string, 0, len(acts)+1)
	for _, a := range acts {
		options = append(options, a.Name)
	}
	if allowNew {
		options = append(options, "+ new activity")
	}
	if len(options) == 0 {
		// No activities yet — straight into create-new prompt.
		s, err := cli.Prompt(cli.Stdin, cli.Stderr, "new activity name", "")
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, cli.ErrCancelled
		}
		a, err := d.CreateActivity(ctx, 0, s)
		if err != nil {
			return nil, err
		}
		return &a, nil
	}
	idx, err := cli.Choose(cli.Stdin, cli.Stderr, label, options, 0)
	if err != nil {
		return nil, err
	}
	if idx < 0 {
		return nil, cli.ErrCancelled
	}
	if allowNew && idx == len(options)-1 {
		s, err := cli.Prompt(cli.Stdin, cli.Stderr, "new activity name", "")
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, cli.ErrCancelled
		}
		a, err := d.CreateActivity(ctx, 0, s)
		if err != nil {
			return nil, err
		}
		return &a, nil
	}
	a := acts[idx]
	return &a, nil
}

// flagWasSet reports whether the user actually set a flag (vs. just the
// default zero value).
func flagWasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func runLog(args []string) {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	periodFlag := fs.String("period", "", "today|yesterday|week|last_week|month|last_month|custom")
	activityFlag := fs.String("activity", "", "filter by activity name")
	startFlag := fs.String("start", "", "start NL (for custom period)")
	endFlag := fs.String("end", "", "end NL (for custom period)")
	_ = fs.Parse(args)

	d, ctx := openDB()
	defer d.Close()
	now := time.Now()

	period, err := resolvePeriodCLI(fs, *periodFlag, *startFlag, *endFlag, now)
	if err != nil {
		fatal("%v", err)
	}

	var actID *int64
	if *activityFlag != "" {
		a, err := d.FindActivityByName(ctx, 0, *activityFlag)
		if err != nil {
			fatal("activity %q: %v", *activityFlag, err)
		}
		actID = &a.ID
	}

	sessions, err := d.ListClosedSessionsInRange(ctx, 0, period.Start, period.End, actID)
	if err != nil {
		fatal("list: %v", err)
	}
	if len(sessions) == 0 {
		fmt.Printf("no sessions in %s\n", period.Label)
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ACTIVITY\tSTART\tEND\tDURATION\tNOTE\n")
	total := 0
	for _, as := range sessions {
		clipped := clip(as.Session, period.Start, period.End)
		if clipped <= 0 {
			continue
		}
		total += clipped
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			as.Activity.Name,
			as.Session.StartAt.Local().Format("01-02 15:04"),
			as.Session.EndAt.Local().Format("01-02 15:04"),
			shortDur(time.Duration(clipped)*time.Second),
			deref(as.Session.Note),
		)
	}
	fmt.Fprintf(tw, "\t\t\t%s\t\n", shortDur(time.Duration(total)*time.Second))
	_ = tw.Flush()
}

func runStats(args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	periodFlag := fs.String("period", "", "today|yesterday|week|last_week|month|last_month|custom")
	startFlag := fs.String("start", "", "start NL (for custom period)")
	endFlag := fs.String("end", "", "end NL (for custom period)")
	_ = fs.Parse(args)

	d, ctx := openDB()
	defer d.Close()
	now := time.Now()

	period, err := resolvePeriodCLI(fs, *periodFlag, *startFlag, *endFlag, now)
	if err != nil {
		fatal("%v", err)
	}

	sessions, err := d.ListClosedSessionsInRange(ctx, 0, period.Start, period.End, nil)
	if err != nil {
		fatal("list: %v", err)
	}

	agg := map[string]int{}
	total := 0
	for _, as := range sessions {
		c := clip(as.Session, period.Start, period.End)
		if c <= 0 {
			continue
		}
		agg[as.Activity.Name] += c
		total += c
	}
	if len(agg) == 0 {
		fmt.Printf("no data in %s\n", period.Label)
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ACTIVITY\tTIME\tSHARE\n")
	type row struct {
		name string
		sec  int
	}
	rows := make([]row, 0, len(agg))
	for n, s := range agg {
		rows = append(rows, row{n, s})
	}
	// sort desc
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].sec > rows[i].sec {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	for _, r := range rows {
		share := 0.0
		if total > 0 {
			share = float64(r.sec) / float64(total) * 100
		}
		fmt.Fprintf(tw, "%s\t%s\t%.1f%%\n", r.name, shortDur(time.Duration(r.sec)*time.Second), share)
	}
	fmt.Fprintf(tw, "\t%s\t\n", shortDur(time.Duration(total)*time.Second))
	_ = tw.Flush()
}

// resolvePeriodCLI parses --period and falls back to an interactive picker
// when not provided. Custom range uses --start/--end (NL) or prompts.
func resolvePeriodCLI(fs *flag.FlagSet, period, startStr, endStr string, now time.Time) (timeparse.Period, error) {
	if period != "" {
		if period == "custom" {
			return resolveCustom(startStr, endStr, now)
		}
		return timeparse.ResolvePeriod(period, now)
	}
	idx, err := cli.Choose(cli.Stdin, cli.Stderr, "period?",
		[]string{"today", "yesterday", "this week", "last week", "this month", "last month", "custom"}, 0)
	if err != nil {
		return timeparse.Period{}, err
	}
	names := []string{"today", "yesterday", "week", "last_week", "month", "last_month"}
	if idx == len(names) {
		return resolveCustom("", "", now)
	}
	return timeparse.ResolvePeriod(names[idx], now)
}

func resolveCustom(startStr, endStr string, now time.Time) (timeparse.Period, error) {
	if startStr == "" {
		s, err := cli.Prompt(cli.Stdin, cli.Stderr, "start (NL, e.g. '2025-10-01 09:00')", "")
		if err != nil {
			return timeparse.Period{}, err
		}
		startStr = s
	}
	if endStr == "" {
		s, err := cli.Prompt(cli.Stdin, cli.Stderr, "end (NL, e.g. 'yesterday 23:59')", "now")
		if err != nil {
			return timeparse.Period{}, err
		}
		endStr = s
	}
	start, err := timeparse.ParseDateTime(startStr, now)
	if err != nil {
		return timeparse.Period{}, fmt.Errorf("parse start: %w", err)
	}
	end, err := timeparse.ParseDateTime(endStr, now)
	if err != nil {
		return timeparse.Period{}, fmt.Errorf("parse end: %w", err)
	}
	if !end.After(start) {
		return timeparse.Period{}, fmt.Errorf("end must be after start")
	}
	return timeparse.Period{Start: start, End: end, Label: "custom"}, nil
}

// clip returns the seconds of session s that fall within [start, end].
// Returns 0 if the session is entirely outside the window.
func clip(s model.Session, start, end time.Time) int {
	if s.EndAt == nil {
		return 0
	}
	sStart := s.StartAt
	sEnd := *s.EndAt
	if sEnd.Before(start) || sStart.After(end) {
		return 0
	}
	if sStart.Before(start) {
		sStart = start
	}
	if sEnd.After(end) {
		sEnd = end
	}
	secs := int(sEnd.Sub(sStart).Seconds())
	if secs < 0 {
		return 0
	}
	return secs
}

// deref returns "" for nil pointer, else the string value.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// containsFold is a tiny helper to keep main.go self-contained.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// runGoal dispatches subcommands: set | list | unset.
func runGoal(args []string) {
	if len(args) == 0 {
		printGoalUsage()
		return
	}
	switch args[0] {
	case "set":
		runGoalSet(args[1:])
	case "list", "ls":
		runGoalList()
	case "unset", "rm", "delete":
		runGoalUnset(args[1:])
	case "-h", "--help", "help":
		printGoalUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown goal subcommand %q\n\n", args[0])
		printGoalUsage()
		os.Exit(2)
	}
}

func printGoalUsage() {
	fmt.Println(`paratrack goal — set and track per-activity targets

Usage:
  paratrack goal set --activity <name> --daily <duration>     set / replace a goal
  paratrack goal set --activity <name> --weekly <duration>
  paratrack goal set --activity <name> --monthly <duration>
  paratrack goal list                                          show goals + progress
  paratrack goal unset --activity <name> [--daily|--weekly|--monthly]`)
}

func runGoalSet(args []string) {
	fs := flag.NewFlagSet("goal set", flag.ExitOnError)
	activityFlag := fs.String("activity", "", "activity name (required)")
	dailyFlag := fs.String("daily", "", "daily target (e.g. 2h, 30m)")
	weeklyFlag := fs.String("weekly", "", "weekly target")
	monthlyFlag := fs.String("monthly", "", "monthly target")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *activityFlag == "" {
		fmt.Fprintln(os.Stderr, "usage: paratrack goal set --activity <name> --daily 2h")
		os.Exit(2)
	}

	// Collect (period, duration-string) pairs from whichever flags are set.
	type pair struct{ period, dur string }
	var pairs []pair
	if *dailyFlag != "" {
		pairs = append(pairs, pair{"daily", *dailyFlag})
	}
	if *weeklyFlag != "" {
		pairs = append(pairs, pair{"weekly", *weeklyFlag})
	}
	if *monthlyFlag != "" {
		pairs = append(pairs, pair{"monthly", *monthlyFlag})
	}
	if len(pairs) == 0 {
		fmt.Fprintln(os.Stderr, "specify one of --daily, --weekly, --monthly")
		os.Exit(2)
	}

	d, ctx := openDB()
	defer d.Close()

	act, err := d.GetOrCreateActivity(ctx, 0, *activityFlag)
	if err != nil {
		fatal("activity %q: %v", *activityFlag, err)
	}

	for _, p := range pairs {
		secs, err := timeparse.ParseDuration(p.dur)
		if err != nil {
			fatal("parse %s duration %q: %v", p.period, p.dur, err)
		}
		mins := secs / 60
		g, err := d.UpsertGoal(ctx, 0, act.ID, p.period, mins)
		if err != nil {
			fatal("upsert %s goal: %v", p.period, err)
		}
		fmt.Printf("set %s goal for %q: %d min/%s\n",
			p.period, act.Name, g.TargetMinutes, p.period)
	}
}

func runGoalList() {
	d, ctx := openDB()
	defer d.Close()
	now := time.Now()
	progress, err := d.ProgressForGoals(ctx, 0, now)
	if err != nil {
		fatal("progress: %v", err)
	}
	if len(progress) == 0 {
		fmt.Println("No goals configured. Set one with: paratrack goal set reading --daily 2h")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ACTIVITY\tPERIOD\tPROGRESS\tTARGET\t%")
	for _, p := range progress {
		achieved := fmtDurationMinutes(p.AchievedMinutes)
		target := fmtDurationMinutes(p.Goal.TargetMinutes)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d%%\n",
			p.ActivityName, p.Goal.Period, achieved, target, p.PercentComplete)
	}
	tw.Flush()
}

func runGoalUnset(args []string) {
	fs := flag.NewFlagSet("goal unset", flag.ExitOnError)
	activityFlag := fs.String("activity", "", "activity name (required)")
	dailyFlag := fs.Bool("daily", false, "remove the daily goal")
	weeklyFlag := fs.Bool("weekly", false, "remove the weekly goal")
	monthlyFlag := fs.Bool("monthly", false, "remove the monthly goal")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *activityFlag == "" {
		fmt.Fprintln(os.Stderr, "usage: paratrack goal unset --activity <name> [--daily]")
		os.Exit(2)
	}

	periods := []string{}
	if *dailyFlag {
		periods = append(periods, "daily")
	}
	if *weeklyFlag {
		periods = append(periods, "weekly")
	}
	if *monthlyFlag {
		periods = append(periods, "monthly")
	}
	if len(periods) == 0 {
		// No flag: remove all periods for this activity.
		periods = []string{"daily", "weekly", "monthly"}
	}

	d, ctx := openDB()
	defer d.Close()
	act, err := d.GetActivityByName(ctx, 0, *activityFlag)
	if err != nil {
		fatal("activity %q: %v", *activityFlag, err)
	}
	for _, p := range periods {
		if err := d.DeleteGoal(ctx, 0, act.ID, p); err != nil && !errors.Is(err, db.ErrGoalNotFound) {
			fatal("unset %s: %v", p, err)
		}
	}
	fmt.Printf("removed %s goals for %q\n", strings.Join(periods, ","), act.Name)
}

// fmtDurationMinutes renders N minutes as "Xh YYm" or "Ym".
func fmtDurationMinutes(min int) string {
	if min < 60 {
		return fmt.Sprintf("%dm", min)
	}
	h := min / 60
	m := min % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// runTag dispatches: add | list | attach | detach.
func runTag(args []string) {
	if len(args) == 0 {
		printTagUsage()
		return
	}
	switch args[0] {
	case "add":
		runTagAdd(args[1:])
	case "list", "ls":
		runTagList()
	case "attach":
		runTagAttach(args[1:])
	case "detach", "rm":
		runTagDetach(args[1:])
	case "-h", "--help", "help":
		printTagUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown tag subcommand %q\n\n", args[0])
		printTagUsage()
		os.Exit(2)
	}
}

func printTagUsage() {
	fmt.Println(`paratrack tag — free-form labels for sessions

Usage:
  paratrack tag add <name>                            create a tag
  paratrack tag list                                  list tags + counts
  paratrack tag attach <session_id> <name>            tag a session
  paratrack tag detach <session_id> <name>            untag`)
}

func runTagAdd(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: paratrack tag add <name>")
		os.Exit(2)
	}
	d, ctx := openDB()
	defer d.Close()
	t, err := d.CreateTag(ctx, 0, args[0])
	if err != nil {
		fatal("create tag: %v", err)
	}
	fmt.Printf("tag #%s ready (id=%d)\n", t.Name, t.ID)
}

func runTagList() {
	d, ctx := openDB()
	defer d.Close()
	tags, err := d.ListAllTagsWithCounts(ctx, 0)
	if err != nil {
		fatal("list tags: %v", err)
	}
	if len(tags) == 0 {
		fmt.Println("No tags yet. Create one with: paratrack tag add deep-work")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TAG\tSESSIONS")
	for _, t := range tags {
		fmt.Fprintf(tw, "#%s\t%d\n", t.Name, t.SessionCount)
	}
	tw.Flush()
}

func runTagAttach(args []string) {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: paratrack tag attach <session_id> <name>")
		os.Exit(2)
	}
	sid, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		fatal("session id: %v", err)
	}
	d, ctx := openDB()
	defer d.Close()
	if err := d.AttachTag(ctx, 0, sid, args[1]); err != nil {
		fatal("attach: %v", err)
	}
	fmt.Printf("tagged session %d with #%s\n", sid, args[1])
}

func runTagDetach(args []string) {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: paratrack tag detach <session_id> <name>")
		os.Exit(2)
	}
	sid, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		fatal("session id: %v", err)
	}
	d, ctx := openDB()
	defer d.Close()
	if err := d.DetachTag(ctx, 0, sid, args[1]); err != nil {
		fatal("detach: %v", err)
	}
	fmt.Printf("removed #%s from session %d\n", args[1], sid)
}

func runWeb(args []string) {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	addr := fs.String("addr", "127.0.0.1:8000", "address to listen on")
	open := fs.Bool("open", false, "open the UI in the default browser once ready")
	_ = fs.Parse(args)

	d, ctx := openDB()
	defer d.Close()
	_ = ctx

	srv, err := web.New(d, *addr)
	if err != nil {
		fatal("init web server: %v", err)
	}
	fmt.Printf("paratrack web: http://%s\n", *addr)
	if *open {
		url := "http://" + *addr
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(url)
		}()
	}
	if err := srv.ListenAndServe(); err != nil {
		fatal("serve: %v", err)
	}
}

// openBrowser asks the OS to open a URL. Best-effort: if it fails
// (e.g. on a headless server), we silently continue — the server is
// still reachable from the printed URL.
func openBrowser(u string) {
	exe, err := lookupBrowserCmd()
	if err != nil || exe == "" {
		return
	}
	_ = exec.Command(exe, u).Start()
}

func lookupBrowserCmd() (string, error) {
	for _, cand := range []string{"open", "xdg-open", "wslview"} {
		if p, err := exec.LookPath(cand); err == nil {
			return p, nil
		}
	}
	return "", nil
}

// equalsFold is a tiny case-insensitive compare that avoids pulling in
// strings just for one call site.
func equalsFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// --- project CLI -----------------------------------------------------
//
// The CLI runs without an auth context, so it doesn't know which team
// to scope to. We pick the lowest-id team by default — sufficient for
// single-user installs. If you have multiple teams, pass --team.
//
// All subcommands accept either a slug ("eora-rag") or a numeric id.

func runProject(args []string) {
	if len(args) == 0 {
		printProjectUsage()
		return
	}
	switch args[0] {
	case "list", "ls":
		runProjectList(args[1:])
	case "create", "new":
		runProjectCreate(args[1:])
	case "show":
		runProjectShow(args[1:])
	case "rename":
		runProjectRename(args[1:])
	case "color":
		runProjectColor(args[1:])
	case "archive":
		runProjectArchive(args[1:], true)
	case "unarchive":
		runProjectArchive(args[1:], false)
	case "delete", "rm":
		runProjectDelete(args[1:])
	case "-h", "--help", "help":
		printProjectUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown project subcommand %q\n\n", args[0])
		printProjectUsage()
		os.Exit(2)
	}
}

func printProjectUsage() {
	fmt.Println(`paratrack project — group activities under named projects

Usage:
  paratrack project list [--team <id>] [--archived]
  paratrack project create <name> [--slug <slug>] [--color <#hex>] [--team <id>]
  paratrack project show <slug|id>
  paratrack project rename <slug|id> <new-name>
  paratrack project color <slug|id> <#hex>
  paratrack project archive <slug|id>
  paratrack project unarchive <slug|id>
  paratrack project delete <slug|id>

Projects belong to a team. --team defaults to the first team in the DB
(single-user installs). Slugs are auto-derived from the name unless given.`)
}

func projectDefaultTeam(d *db.DB) (int64, error) {
	var id int64
	err := d.SQL().QueryRow(`SELECT id FROM teams ORDER BY id LIMIT 1`).Scan(&id)
	return id, err
}

// parseProjectRef resolves a slug or numeric id to a project id.
func parseProjectRef(d *db.DB, ref string) (int64, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return id, nil
	}
	var id int64
	err := d.SQL().QueryRow(`SELECT id FROM projects WHERE slug = ? COLLATE NOCASE`, ref).Scan(&id)
	return id, err
}

func runProjectList(args []string) {
	fs := flag.NewFlagSet("project list", flag.ExitOnError)
	team := fs.Int64("team", 0, "team id (default: first team)")
	archived := fs.Bool("archived", false, "include archived projects")
	fs.Parse(args)

	d, ctx := openDB()
	defer d.Close()
	if *team == 0 {
		t, err := projectDefaultTeam(d)
		if err != nil {
			fatal("no team found; pass --team or seed one via the web UI: %v", err)
		}
		*team = t
	}
	list, err := d.ListProjects(ctx, *team, *archived)
	if err != nil {
		fatal("list: %v", err)
	}
	if len(list) == 0 {
		fmt.Println("No projects yet. Create one with: paratrack project create \"EORA RAG\"")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SLUG\tNAME\tCOLOR\tACTIVITIES\tSTATE")
	for _, p := range list {
		acts, _ := d.ListActivitiesForProject(ctx, p.ID, true)
		state := "active"
		if p.Archived {
			state = "archived"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", p.Slug, p.Name, p.Color, len(acts), state)
	}
	tw.Flush()
}

func runProjectCreate(args []string) {
	fs := flag.NewFlagSet("project create", flag.ExitOnError)
	slug := fs.String("slug", "", "URL slug (default: derived from name)")
	color := fs.String("color", "", "color hex like #7c3aed (default: #7c8499)")
	team := fs.Int64("team", 0, "team id (default: first team)")
	fs.Parse(args)
	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "usage: paratrack project create <name> [--slug s] [--color #hex]")
		os.Exit(2)
	}

	d, ctx := openDB()
	defer d.Close()
	if *team == 0 {
		t, err := projectDefaultTeam(d)
		if err != nil {
			fatal("no team found; pass --team: %v", err)
		}
		*team = t
	}
	p, err := d.CreateProject(ctx, *team, rest[0], *slug, *color)
	if err != nil {
		if errors.Is(err, db.ErrDuplicate) {
			fatal("a project with that slug or name already exists in this team")
		}
		fatal("create: %v", err)
	}
	fmt.Printf("project #%d ready (slug=%s, color=%s, team=%d)\n", p.ID, p.Slug, p.Color, p.TeamID)
}

func runProjectShow(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: paratrack project show <slug|id>")
		os.Exit(2)
	}
	d, ctx := openDB()
	defer d.Close()
	id, err := parseProjectRef(d, args[0])
	if err != nil {
		fatal("project not found: %v", err)
	}
	p, err := d.GetProject(ctx, id)
	if err != nil {
		fatal("get: %v", err)
	}
	fmt.Printf("id       %d\n", p.ID)
	fmt.Printf("team_id  %d\n", p.TeamID)
	fmt.Printf("slug     %s\n", p.Slug)
	fmt.Printf("name     %s\n", p.Name)
	fmt.Printf("color    %s\n", p.Color)
	fmt.Printf("state    %s\n", stateStr(p.Archived))
	acts, _ := d.ListActivitiesForProject(ctx, p.ID, true)
	if len(acts) > 0 {
		fmt.Println("activities:")
		for _, a := range acts {
			marker := ""
			if a.Archived {
				marker = " (archived)"
			}
			fmt.Printf("  - %s%s\n", a.Name, marker)
		}
	}
}

func runProjectRename(args []string) {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: paratrack project rename <slug|id> <new-name>")
		os.Exit(2)
	}
	d, ctx := openDB()
	defer d.Close()
	id, err := parseProjectRef(d, args[0])
	if err != nil {
		fatal("project not found: %v", err)
	}
	p, err := d.GetProject(ctx, id)
	if err != nil {
		fatal("get: %v", err)
	}
	upd, err := d.UpdateProject(ctx, p.TeamID, id, args[1], "", nil, nil)
	if err != nil {
		fatal("rename: %v", err)
	}
	fmt.Printf("renamed #%d → %q\n", upd.ID, upd.Name)
}

func runProjectColor(args []string) {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: paratrack project color <slug|id> <#hex>")
		os.Exit(2)
	}
	d, ctx := openDB()
	defer d.Close()
	id, err := parseProjectRef(d, args[0])
	if err != nil {
		fatal("project not found: %v", err)
	}
	p, err := d.GetProject(ctx, id)
	if err != nil {
		fatal("get: %v", err)
	}
	upd, err := d.UpdateProject(ctx, p.TeamID, id, "", args[1], nil, nil)
	if err != nil {
		fatal("color: %v", err)
	}
	fmt.Printf("color #%d → %s\n", upd.ID, upd.Color)
}

func runProjectArchive(args []string, archive bool) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: paratrack project archive|unarchive <slug|id>")
		os.Exit(2)
	}
	d, ctx := openDB()
	defer d.Close()
	id, err := parseProjectRef(d, args[0])
	if err != nil {
		fatal("project not found: %v", err)
	}
	p, err := d.GetProject(ctx, id)
	if err != nil {
		fatal("get: %v", err)
	}
	verb := "unarchived"
	if archive {
		verb = "archived"
	}
	if _, err := d.UpdateProject(ctx, p.TeamID, id, "", "", &archive, nil); err != nil {
		fatal("set archived: %v", err)
	}
	fmt.Printf("%s #%d\n", verb, id)
}

func runProjectDelete(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: paratrack project delete <slug|id>")
		os.Exit(2)
	}
	d, ctx := openDB()
	defer d.Close()
	id, err := parseProjectRef(d, args[0])
	if err != nil {
		fatal("project not found: %v", err)
	}
	p, err := d.GetProject(ctx, id)
	if err != nil {
		fatal("get: %v", err)
	}
	if err := d.DeleteProject(ctx, p.TeamID, id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			fatal("not found")
		}
		fatal("delete: %v", err)
	}
	fmt.Printf("deleted #%d (activities in it are now 'Uncategorized')\n", id)
}

func stateStr(archived bool) string {
	if archived {
		return "archived"
	}
	return "active"
}


// runMigrateToPostgres copies a SQLite database into the empty Postgres
// database at PARATRACK_DATABASE_URL (schema is created on open).
func runMigrateToPostgres(args []string) {
	if len(args) != 1 {
		fatal("usage: paratrack migrate-to-postgres <path/to/track.db>")
	}
	url := os.Getenv("PARATRACK_DATABASE_URL")
	if url == "" {
		fatal("set PARATRACK_DATABASE_URL to the target Postgres")
	}
	if _, err := os.Stat(args[0]); err != nil {
		fatal("source: %v", err)
	}
	src, err := db.Open(args[0])
	if err != nil {
		fatal("open %s: %v", args[0], err)
	}
	defer src.Close()
	dst, err := db.OpenPostgres(url)
	if err != nil {
		fatal("open postgres: %v", err)
	}
	defer dst.Close()
	counts, err := db.CopyToPostgres(context.Background(), src, dst)
	if err != nil {
		fatal("copy: %v", err)
	}
	total := 0
	for table, n := range counts {
		if n > 0 {
			fmt.Printf("%-24s %d\n", table, n)
		}
		total += n
	}
	fmt.Printf("copied %d rows from %d tables\n", total, len(counts))
}
