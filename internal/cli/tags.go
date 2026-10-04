package cli

import (
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

// runTag dispatches: add | list | attach | detach.
func runTag(rt *Runtime, args []string) error {
	if len(args) == 0 {
		printTagUsage(rt)
		return nil
	}
	switch args[0] {
	case "add":
		return runTagAdd(rt, args[1:])
	case "list", "ls":
		return runTagList(rt)
	case "attach":
		return runTagAttach(rt, args[1:])
	case "detach", "rm":
		return runTagDetach(rt, args[1:])
	case "-h", "--help", "help":
		printTagUsage(rt)
	default:
		fmt.Fprintf(rt.Err, "unknown tag subcommand %q\n\n", args[0])
		printTagUsage(rt)
		return &ExitError{Code: 2}
	}
	return nil
}

func printTagUsage(rt *Runtime) {
	fmt.Fprintln(rt.Out, `paratrack tag — free-form labels for sessions

Usage:
  paratrack tag add <name>                            create a tag
  paratrack tag list                                  list tags + counts
  paratrack tag attach <session_id> <name>            tag a session
  paratrack tag detach <session_id> <name>            untag`)
}

func runTagAdd(rt *Runtime, args []string) error {
	if len(args) != 1 {
		return &ExitError{Code: 2, Err: fmt.Errorf("usage: paratrack tag add <name>")}
	}
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	t, err := services.Tagging.CreateForMember(ctx, appmodel.TagCreateRequest{TeamID: teamID, CallerID: requestctx.ActorID(ctx), Name: args[0]})
	if err != nil {
		return fmt.Errorf("create tag: %w", err)
	}
	fmt.Fprintf(rt.Out, "tag #%s ready (id=%d)\n", t.Name, t.ID)
	return nil
}

func runTagList(rt *Runtime) error {
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	tags, err := services.Tagging.ListWithCounts(ctx, teamID)
	if err != nil {
		return fmt.Errorf("list tags: %w", err)
	}
	if len(tags) == 0 {
		fmt.Fprintln(rt.Out, "No tags yet. Create one with: paratrack tag add deep-work")
		return nil
	}
	tw := tabwriter.NewWriter(rt.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TAG\tSESSIONS")
	for _, t := range tags {
		fmt.Fprintf(tw, "#%s\t%d\n", t.Name, t.SessionCount)
	}
	tw.Flush()
	return nil
}

func runTagAttach(rt *Runtime, args []string) error {
	if len(args) != 2 {
		return &ExitError{Code: 2, Err: fmt.Errorf("usage: paratrack tag attach <session_id> <name>")}
	}
	sid, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("session id: %w", err)
	}
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	if err := services.Tagging.AttachForMember(ctx, appmodel.SessionTagRequest{TeamID: teamID, CallerID: requestctx.ActorID(ctx), SessionID: sid, Name: args[1]}); err != nil {
		return fmt.Errorf("attach: %w", err)
	}
	fmt.Fprintf(rt.Out, "tagged session %d with #%s\n", sid, args[1])
	return nil
}

func runTagDetach(rt *Runtime, args []string) error {
	if len(args) != 2 {
		return &ExitError{Code: 2, Err: fmt.Errorf("usage: paratrack tag detach <session_id> <name>")}
	}
	sid, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("session id: %w", err)
	}
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	teamID, ctx, err := defaultTeamForCommand(services, ctx)
	if err != nil {
		return err
	}
	if err := services.Tagging.DetachForMember(ctx, appmodel.SessionTagRequest{TeamID: teamID, CallerID: requestctx.ActorID(ctx), SessionID: sid, Name: args[1]}); err != nil {
		return fmt.Errorf("detach: %w", err)
	}
	fmt.Fprintf(rt.Out, "removed #%s from session %d\n", args[1], sid)
	return nil
}
