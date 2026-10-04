package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/requestctx"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

func runAdd(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	activityFlag := fs.String("activity", "", "activity name (skip interactive picker)")
	startFlag := fs.String("start", "", "start time NL (e.g. '2 hours ago')")
	modeFlag := fs.String("mode", "", "duration | end (skip prompt)")
	durationFlag := fs.String("duration", "", "duration NL (e.g. '1h 30m')")
	endFlag := fs.String("end", "", "end time NL")
	noteFlag := fs.String("note", "", "session note")
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

	act, err := resolveAddActivity(rt, ctx, services.ActivityCatalog, teamID, *activityFlag)
	if errors.Is(err, ErrCancelled) {
		fmt.Fprintln(rt.Out, "cancelled")
		return nil
	}
	if err != nil {
		return err
	}
	startAt, endAt, err := resolveAddInterval(rt, *startFlag, *modeFlag, *durationFlag, *endFlag, now)
	if err != nil {
		return err
	}
	note := *noteFlag
	if !flagWasSet(fs, "note") {
		if entered, err := Prompt(rt.In, rt.Err, "note (optional)", ""); err == nil {
			note = entered
		}
	}

	_, err = services.TimerOperations.AddClosed(ctx, appmodel.TimerAddByIDRequest{
		TeamID: teamID, CallerID: requestctx.ActorID(ctx), ActivityID: act.ID,
		Start: startAt, End: endAt, Note: note,
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	fmt.Fprintf(rt.Out, "✓ added %q %s → %s\n", act.Name,
		startAt.Format("2006-01-02 15:04"), endAt.Format("2006-01-02 15:04"))
	return nil
}

func resolveAddActivity(rt *Runtime, ctx context.Context, catalog activityCatalog, teamID int64, name string) (model.Activity, error) {
	if name != "" {
		activity, err := catalog.ResolveActivityForMember(ctx, appmodel.ActivityResolveRequest{
			TeamID: teamID, CallerID: requestctx.ActorID(ctx), Name: name,
		})
		if err != nil {
			return model.Activity{}, fmt.Errorf("get-or-create activity: %w", err)
		}
		return activity, nil
	}
	activity, err := pickActivity(rt, ctx, catalog, teamID, "activity (or new):", true)
	if err != nil {
		if errors.Is(err, ErrCancelled) {
			return model.Activity{}, ErrCancelled
		}
		return model.Activity{}, fmt.Errorf("select activity: %w", err)
	}
	if activity == nil {
		return model.Activity{}, ErrCancelled
	}
	return *activity, nil
}

func resolveAddInterval(rt *Runtime, startValue, mode, durationValue, endValue string, now time.Time) (time.Time, time.Time, error) {
	if startValue == "" {
		entered, err := Prompt(rt.In, rt.Err, "start (e.g. '2 hours ago', 'yesterday 14:00')", "2 hours ago")
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("read start: %w", err)
		}
		startValue = entered
	}
	start, err := timeparse.ParseDateTime(startValue, now)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("parse start %q: %w", startValue, err)
	}
	if mode == "" {
		choice, err := Choose(rt.In, rt.Err, "provide duration or end time?",
			[]string{"duration (e.g. 1h, 90m)", "end time (e.g. 'yesterday 16:30')"}, 0)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("read mode: %w", err)
		}
		if choice == 0 {
			mode = "duration"
		} else {
			mode = "end"
		}
	}
	var end time.Time
	switch mode {
	case "duration":
		if durationValue == "" {
			durationValue, err = Prompt(rt.In, rt.Err, "duration", "1h")
			if err != nil {
				return time.Time{}, time.Time{}, fmt.Errorf("read duration: %w", err)
			}
		}
		seconds, err := timeparse.ParseDuration(durationValue)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse duration %q: %w", durationValue, err)
		}
		end = start.Add(time.Duration(seconds) * time.Second)
	case "end":
		if endValue == "" {
			endValue, err = Prompt(rt.In, rt.Err, "end time (e.g. 'yesterday 16:30')", "")
			if err != nil {
				return time.Time{}, time.Time{}, fmt.Errorf("read end: %w", err)
			}
		}
		end, err = timeparse.ParseDateTime(endValue, now)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parse end %q: %w", endValue, err)
		}
		if !end.After(start) {
			return time.Time{}, time.Time{}, errors.New("end time must be after start time")
		}
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("unknown mode %q (use 'duration' or 'end')", mode)
	}
	return start, end, nil
}

// pickActivity shows a numbered list of known activities and asks the user
// to pick one. When allowNew is true, the last option is "+ new".
// Returns (nil, ErrCancelled) if the user types ".".
type activityCatalog interface {
	Activities(context.Context, int64, bool) ([]model.Activity, error)
	ResolveActivityForMember(context.Context, appmodel.ActivityResolveRequest) (model.Activity, error)
}

func pickActivity(rt *Runtime, ctx context.Context, catalog activityCatalog, teamID int64, label string, allowNew bool) (*model.Activity, error) {
	acts, err := catalog.Activities(ctx, teamID, false)
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

		s, err := Prompt(rt.In, rt.Err, "new activity name", "")
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, ErrCancelled
		}
		a, err := catalog.ResolveActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: requestctx.ActorID(ctx), Name: s})
		if err != nil {
			return nil, err
		}
		return &a, nil
	}
	idx, err := Choose(rt.In, rt.Err, label, options, 0)
	if err != nil {
		return nil, err
	}
	if idx < 0 {
		return nil, ErrCancelled
	}
	if allowNew && idx == len(options)-1 {
		s, err := Prompt(rt.In, rt.Err, "new activity name", "")
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, ErrCancelled
		}
		a, err := catalog.ResolveActivityForMember(ctx, appmodel.ActivityResolveRequest{TeamID: teamID, CallerID: requestctx.ActorID(ctx), Name: s})
		if err != nil {
			return nil, err
		}
		return &a, nil
	}
	a := acts[idx]
	return &a, nil
}
