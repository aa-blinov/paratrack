package cli

import (
	"flag"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/money"
	"github.com/aa-blinov/paratrack/internal/reportstats"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

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

func runLog(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	periodFlag := fs.String("period", "", "today|yesterday|week|last_week|month|last_month|custom")
	activityFlag := fs.String("activity", "", "filter by activity name")
	startFlag := fs.String("start", "", "start NL (for custom period)")
	endFlag := fs.String("end", "", "end NL (for custom period)")
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}

	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	now := rt.now()

	period, err := resolvePeriodCLI(rt, fs, *periodFlag, *startFlag, *endFlag, now)
	if err != nil {
		return err
	}

	var actID *int64
	if *activityFlag != "" {
		a, err := services.ActivityCatalog.FindActivity(ctx, appmodel.ActivityNameQuery{TeamID: teamID, Name: *activityFlag})
		if err != nil {
			return fmt.Errorf("activity %q: %w", *activityFlag, err)
		}
		actID = &a.ID
	}

	sessions, err := services.SessionHistory.ClosedSessions(ctx, teamID, period.Start, period.End, actID)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Fprintf(rt.Out, "no sessions in %s\n", period.Label)
		return nil
	}

	tw := tabwriter.NewWriter(rt.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ACTIVITY\tSTART\tEND\tDURATION\tNOTE\n")
	total := 0
	for _, as := range sessions {
		clipped := as.Session.TrackedSecondsInWindow(period.Start, period.End, now)
		if clipped <= 0 {
			continue
		}
		total, err = money.AddInt(total, clipped)
		if err != nil {
			return fmt.Errorf("sum log duration: %w", err)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			as.Activity.Name,
			as.Session.StartAt.In(now.Location()).Format("01-02 15:04"),
			as.Session.EndAt.In(now.Location()).Format("01-02 15:04"),
			shortDurSeconds(clipped),
			deref(as.Session.Note),
		)
	}
	fmt.Fprintf(tw, "\t\t\t%s\t\n", shortDurSeconds(total))
	_ = tw.Flush()
	return nil
}

func runStats(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	periodFlag := fs.String("period", "", "today|yesterday|week|last_week|month|last_month|custom")
	startFlag := fs.String("start", "", "start NL (for custom period)")
	endFlag := fs.String("end", "", "end NL (for custom period)")
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}

	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	now := rt.now()

	period, err := resolvePeriodCLI(rt, fs, *periodFlag, *startFlag, *endFlag, now)
	if err != nil {
		return err
	}

	sessions, err := services.SessionHistory.ClosedSessions(ctx, teamID, period.Start, period.End, nil)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	summary, err := reportstats.Summarize(sessions, nil, period.Start, period.End, now, "Uncategorized")
	if err != nil {
		return fmt.Errorf("summarize report: %w", err)
	}
	if len(summary.Activities) == 0 {
		fmt.Fprintf(rt.Out, "no data in %s\n", period.Label)
		return nil
	}

	tw := tabwriter.NewWriter(rt.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ACTIVITY\tTIME\tSHARE\n")
	for _, row := range summary.Activities {
		fmt.Fprintf(tw, "%s\t%s\t%.1f%%\n", row.Name, shortDurSeconds(row.Seconds), row.Share)
	}
	fmt.Fprintf(tw, "\t%s\t\n", shortDurSeconds(summary.TotalSeconds))
	_ = tw.Flush()
	return nil
}

// resolvePeriodCLI parses --period and falls back to an interactive picker
// when not provided. Custom range uses --start/--end (NL) or prompts.
func resolvePeriodCLI(rt *Runtime, fs *flag.FlagSet, period, startStr, endStr string, now time.Time) (timeparse.Period, error) {
	if period != "" {
		if period == "custom" {
			return resolveCustom(rt, startStr, endStr, now)
		}
		return timeparse.ResolvePeriod(period, now)
	}
	idx, err := Choose(rt.In, rt.Err, "period?",
		[]string{"today", "yesterday", "this week", "last week", "this month", "last month", "custom"}, 0)
	if err != nil {
		return timeparse.Period{}, err
	}
	names := []string{"today", "yesterday", "week", "last_week", "month", "last_month"}
	if idx == len(names) {
		return resolveCustom(rt, "", "", now)
	}
	return timeparse.ResolvePeriod(names[idx], now)
}

func resolveCustom(rt *Runtime, startStr, endStr string, now time.Time) (timeparse.Period, error) {
	if startStr == "" {
		s, err := Prompt(rt.In, rt.Err, "start (NL, e.g. '2025-10-01 09:00')", "")
		if err != nil {
			return timeparse.Period{}, err
		}
		startStr = s
	}
	if endStr == "" {
		s, err := Prompt(rt.In, rt.Err, "end (NL, e.g. 'yesterday 23:59')", "now")
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

// deref returns "" for nil pointer, else the string value.
func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
