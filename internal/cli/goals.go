package cli

import (
	"errors"
	"flag"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/aa-blinov/paratrack/internal/requestctx"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

// runGoal dispatches subcommands: set | list | unset.
func runGoal(rt *Runtime, args []string) error {
	if len(args) == 0 {
		printGoalUsage(rt)
		return nil
	}
	switch args[0] {
	case "set":
		return runGoalSet(rt, args[1:])
	case "list", "ls":
		return runGoalList(rt)
	case "unset", "rm", "delete":
		return runGoalUnset(rt, args[1:])
	case "-h", "--help", "help":
		printGoalUsage(rt)
	default:
		fmt.Fprintf(rt.Err, "unknown goal subcommand %q\n\n", args[0])
		printGoalUsage(rt)
		return &ExitError{Code: 2}
	}
	return nil
}

func printGoalUsage(rt *Runtime) {
	fmt.Fprintln(rt.Out, `paratrack goal — set and track per-activity targets

Usage:
  paratrack goal set --activity <name> --daily <duration>     set / replace a goal
  paratrack goal set --activity <name> --weekly <duration>
  paratrack goal set --activity <name> --monthly <duration>
  paratrack goal list                                          show goals + progress
  paratrack goal unset --activity <name> [--daily|--weekly|--monthly]`)
}

func runGoalSet(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("goal set", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	activityFlag := fs.String("activity", "", "activity name (required)")
	dailyFlag := fs.String("daily", "", "daily target (e.g. 2h, 30m)")
	weeklyFlag := fs.String("weekly", "", "weekly target")
	monthlyFlag := fs.String("monthly", "", "monthly target")
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	if *activityFlag == "" {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack goal set --activity <name> --daily 2h")}
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
		return &ExitError{Code: 2, Err: errors.New("specify one of --daily, --weekly, --monthly")}
	}

	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	targets := make([]appmodel.GoalTarget, 0, len(pairs))
	for _, p := range pairs {
		secs, err := timeparse.ParseDuration(p.dur)
		if err != nil {
			return fmt.Errorf("parse %s duration %q: %w", p.period, p.dur, err)
		}
		targets = append(targets, appmodel.GoalTarget{Period: p.period, Minutes: secs / 60})
	}
	goals, err := services.GoalCommands.SetForManager(ctx, appmodel.GoalSetRequest{
		TeamID: teamID, CallerID: requestctx.ActorID(ctx), ActivityName: *activityFlag, Targets: targets,
	})
	if err != nil {
		return fmt.Errorf("set goals: %w", err)
	}
	for _, g := range goals {
		fmt.Fprintf(rt.Out, "set %s goal for %q: %d min/%s\n",
			g.Period, *activityFlag, g.TargetMinutes, g.Period)
	}
	return nil
}

func runGoalList(rt *Runtime) error {
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	service := services.GoalQueries
	now := rt.now()
	progress, err := service.Progress(ctx, teamID, now)
	if err != nil {
		return fmt.Errorf("progress: %w", err)
	}
	if len(progress) == 0 {
		fmt.Fprintln(rt.Out, "No goals configured. Set one with: paratrack goal set reading --daily 2h")
		return nil
	}
	tw := tabwriter.NewWriter(rt.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ACTIVITY\tPERIOD\tPROGRESS\tTARGET\t%")
	for _, p := range progress {
		achieved := fmtDurationMinutes(p.AchievedMinutes)
		target := fmtDurationMinutes(p.Goal.TargetMinutes)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d%%\n",
			p.ActivityName, p.Goal.Period, achieved, target, p.PercentComplete)
	}
	tw.Flush()
	return nil
}

func runGoalUnset(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("goal unset", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	activityFlag := fs.String("activity", "", "activity name (required)")
	dailyFlag := fs.Bool("daily", false, "remove the daily goal")
	weeklyFlag := fs.Bool("weekly", false, "remove the weekly goal")
	monthlyFlag := fs.Bool("monthly", false, "remove the monthly goal")
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	if *activityFlag == "" {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack goal unset --activity <name> [--daily]")}
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

		periods = []string{"daily", "weekly", "monthly"}
	}

	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	if _, err := services.GoalCommands.UnsetForManager(ctx, appmodel.GoalUnsetRequest{
		TeamID: teamID, CallerID: requestctx.ActorID(ctx), ActivityName: *activityFlag, Periods: periods,
	}); err != nil {
		return fmt.Errorf("unset goals: %w", err)
	}
	fmt.Fprintf(rt.Out, "removed %s goals for %q\n", strings.Join(periods, ","), *activityFlag)
	return nil
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
