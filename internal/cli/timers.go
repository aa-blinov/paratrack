package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func runStatus(rt *Runtime) error {
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	rows, err := services.TimerQueries.ActiveSessions(ctx, teamID)
	if err != nil {
		return fmt.Errorf("list active: %w", err)
	}
	printActive(rt, rows)
	return nil
}

func runStart(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	note := fs.String("note", "", "optional note for the session")
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	if fs.NArg() < 1 {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack start <activity> [--note ...]")}
	}
	name := fs.Arg(0)
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	act, s, err := services.TimerOperations.StartActivity(ctx, appmodel.TimerStartByNameRequest{
		TeamID: teamID, CallerID: requestctx.ActorID(ctx), ActivityName: name, At: rt.now(), Note: *note,
	})
	if err != nil {
		if errors.Is(err, model.ErrActiveSessionExists) {
			return fmt.Errorf("activity %q already has an active session; use stop or pause first", act.Name)
		}
		return fmt.Errorf("start activity: %w", err)
	}
	fmt.Fprintf(rt.Out, "✓ started %q (session #%d)\n", act.Name, s.ID)
	if *note != "" {
		fmt.Fprintf(rt.Out, "  note: %s\n", *note)
	}
	return nil
}

func runStop(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	target := ""
	if fs.NArg() > 0 {
		target = fs.Arg(0)
	}
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	if target == "" {
		stopped, err := services.TimerOperations.StopAll(ctx, appmodel.TimerStopAllRequest{TeamID: teamID, At: rt.now()})
		if err != nil {
			return fmt.Errorf("stop all: %w", err)
		}
		if len(stopped) == 0 {
			fmt.Fprintln(rt.Out, "No active sessions to stop.")
			return nil
		}
		fmt.Fprintf(rt.Out, "✓ stopped %d session(s)\n", len(stopped))
		return nil
	}
	active, err := services.TimerQueries.ActiveSessions(ctx, teamID)
	if err != nil {
		return fmt.Errorf("list active: %w", err)
	}
	stopped := 0
	for _, as := range active {
		if equalsFold(as.Activity.Name, target) {
			if _, err := services.TimerOperations.Stop(ctx, appmodel.TimerStopRequest{TeamID: teamID, SessionID: as.Session.ID, At: rt.now()}); err != nil {
				return fmt.Errorf("stop %d: %w", as.Session.ID, err)
			}
			fmt.Fprintf(rt.Out, "✓ stopped %q\n", as.Activity.Name)
			stopped++
		}
	}
	if stopped == 0 && target != "" {
		return &ExitError{Code: 1, Err: fmt.Errorf("no active session for %q", target)}
	}
	return nil
}

func runPause(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("pause", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	target := ""
	if fs.NArg() > 0 {
		target = fs.Arg(0)
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
	tracker := services.TimerCommands
	if target == "" {
		ids, err := tracker.PauseAll(ctx, appmodel.TimerStopAllRequest{TeamID: teamID, At: now})
		if err != nil {
			return fmt.Errorf("pause all: %w", err)
		}
		if len(ids) == 0 {
			fmt.Fprintln(rt.Out, "Nothing to pause.")
			return nil
		}
		fmt.Fprintf(rt.Out, "✓ paused %d session(s)\n", len(ids))
		return nil
	}
	count := 0
	active, err := services.TimerQueries.ActiveSessions(ctx, teamID)
	if err != nil {
		return fmt.Errorf("list active: %w", err)
	}
	for _, as := range active {
		if as.Session.Paused {
			continue
		}
		if !equalsFold(as.Activity.Name, target) {
			continue
		}
		if _, err := services.TimerCommands.Pause(ctx, appmodel.TimerSessionRequest{TeamID: teamID, SessionID: as.Session.ID, At: now}); err != nil {
			return fmt.Errorf("pause %d: %w", as.Session.ID, err)
		}
		count++
	}
	if count == 0 {
		fmt.Fprintln(rt.Out, "Nothing to pause.")
		return nil
	}
	fmt.Fprintf(rt.Out, "✓ paused %d session(s)\n", count)
	return nil
}

func runResume(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	target := ""
	if fs.NArg() > 0 {
		target = fs.Arg(0)
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
	count := 0
	active, err := services.TimerQueries.ActiveSessions(ctx, teamID)
	if err != nil {
		return fmt.Errorf("list active: %w", err)
	}
	for _, as := range active {
		if !as.Session.Paused {
			continue
		}
		if target != "" && !equalsFold(as.Activity.Name, target) {
			continue
		}
		if _, err := services.TimerCommands.Resume(ctx, appmodel.TimerSessionRequest{TeamID: teamID, SessionID: as.Session.ID, At: now}); err != nil {
			return fmt.Errorf("resume %d: %w", as.Session.ID, err)
		}
		count++
	}
	if count == 0 {
		fmt.Fprintln(rt.Out, "Nothing to resume.")
		return nil
	}
	fmt.Fprintf(rt.Out, "✓ resumed %d session(s)\n", count)
	return nil
}

func runFocus(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("focus", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	if fs.NArg() < 1 {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack focus <activity>")}
	}
	target := fs.Arg(0)
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	act, err := services.ActivityCatalog.ResolveActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: requestctx.ActorID(ctx), Name: target})
	if err != nil {
		return fmt.Errorf("resolve activity: %w", err)
	}
	now := rt.now()
	result, err := services.TimerOperations.Focus(ctx, appmodel.TimerFocusRequest{TeamID: teamID, ActivityID: act.ID, At: now})
	if err != nil {
		return fmt.Errorf("focus: %w", err)
	}
	if result.Started {
		fmt.Fprintf(rt.Out, "✓ focus started on %q (paused %d other)\n", act.Name, result.Paused)
		return nil
	}
	fmt.Fprintf(rt.Out, "✓ focus on %q (resumed %d, paused %d other)\n", act.Name, result.Resumed, result.Paused)
	return nil
}
