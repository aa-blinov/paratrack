package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/cliport"
	"github.com/aa-blinov/paratrack/internal/model"
)

func runProject(rt *Runtime, args []string) error {
	if len(args) == 0 {
		printProjectUsage(rt)
		return nil
	}
	switch args[0] {
	case "list", "ls":
		return runProjectList(rt, args[1:])
	case "create", "new":
		return runProjectCreate(rt, args[1:])
	case "show":
		return runProjectShow(rt, args[1:])
	case "rename":
		return runProjectRename(rt, args[1:])
	case "color":
		return runProjectColor(rt, args[1:])
	case "archive":
		return runProjectArchive(rt, args[1:], true)
	case "unarchive":
		return runProjectArchive(rt, args[1:], false)
	case "delete", "rm":
		return runProjectDelete(rt, args[1:])
	case "-h", "--help", "help":
		printProjectUsage(rt)
		return nil
	default:
		return &ExitError{Code: 2, Err: fmt.Errorf("unknown project subcommand %q", args[0])}
	}
}

func printProjectUsage(rt *Runtime) {
	fmt.Fprintln(rt.Out, `paratrack project — group activities under named projects

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

func projectDefaultTeam(s cliport.WorkspaceContext, ctx context.Context) (int64, error) {
	return s.DefaultTeam(ctx)
}

func projectActorID(s cliport.WorkspaceContext, ctx context.Context, teamID int64) (int64, error) {
	return s.TeamOwnerID(ctx, teamID)
}

// parseProjectRef resolves a slug or numeric id to a project id.
func parseProjectRef(s cliport.ProjectLookup, ctx context.Context, ref string) (int64, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		return id, nil
	}
	return s.ProjectIDBySlug(ctx, ref)
}

func mustProjectID(s cliport.ProjectLookup, ctx context.Context, ref string) (int64, error) {
	id, err := parseProjectRef(s, ctx, ref)
	if errors.Is(err, model.ErrAmbiguousProject) {
		return 0, fmt.Errorf("project slug %q exists in multiple workspaces; use its numeric ID", ref)
	}
	if err != nil {
		return 0, fmt.Errorf("project not found: %w", err)
	}
	return id, nil
}

func runProjectList(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("project list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	team := fs.Int64("team", 0, "team id (default: first team)")
	archived := fs.Bool("archived", false, "include archived projects")
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}

	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	service := services.Projects
	if *team == 0 {
		t, err := projectDefaultTeam(services.Workspace, ctx)
		if err != nil {
			return fmt.Errorf("no team found; pass --team or seed one via the web UI: %w", err)
		}
		*team = t
	}
	snapshot, err := service.ListWithActivityCounts(ctx, *team, *archived)
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	if len(snapshot.Projects) == 0 {
		fmt.Fprintln(rt.Out, "No projects yet. Create one with: paratrack project create \"EORA RAG\"")
		return nil
	}
	tw := tabwriter.NewWriter(rt.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SLUG\tNAME\tCOLOR\tACTIVITIES\tSTATE")
	for _, p := range snapshot.Projects {
		state := "active"
		if p.Archived {
			state = "archived"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", p.Slug, p.Name, p.Color, snapshot.ActivityCounts[p.ID], state)
	}
	tw.Flush()
	return nil
}

func runProjectCreate(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("project create", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	slug := fs.String("slug", "", "URL slug (default: derived from name)")
	color := fs.String("color", "", "color hex like #7c3aed (default: #7c8499)")
	team := fs.Int64("team", 0, "team id (default: first team)")
	if err := fs.Parse(args); err != nil {
		return &ExitError{Code: 2, Err: err}
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack project create <name> [--slug s] [--color #hex]")}
	}

	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	service := services.Projects
	if *team == 0 {
		t, err := projectDefaultTeam(services.Workspace, ctx)
		if err != nil {
			return fmt.Errorf("no team found; pass --team: %w", err)
		}
		*team = t
	}
	callerID, err := projectActorID(services.Workspace, ctx, *team)
	if err != nil {
		return fmt.Errorf("resolve project workspace owner: %w", err)
	}
	p, err := service.Create(ctx, appmodel.ProjectCreateRequest{
		TeamID: *team, CallerID: callerID, Name: rest[0], Slug: *slug, Color: *color,
	})
	if err != nil {
		if errors.Is(err, model.ErrAlreadyExists) {
			return errors.New("a project with that slug or name already exists in this team")
		}
		return fmt.Errorf("create project: %w", err)
	}
	fmt.Fprintf(rt.Out, "project #%d ready (slug=%s, color=%s, team=%d)\n", p.ID, p.Slug, p.Color, p.TeamID)
	return nil
}

func runProjectShow(rt *Runtime, args []string) error {
	if len(args) != 1 {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack project show <slug|id>")}
	}
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	service := services.Projects
	id, err := mustProjectID(services.ProjectLookup, ctx, args[0])
	if err != nil {
		return err
	}
	p, err := services.ProjectLookup.GetProjectByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}
	fmt.Fprintf(rt.Out, "id       %d\n", p.ID)
	fmt.Fprintf(rt.Out, "team_id  %d\n", p.TeamID)
	fmt.Fprintf(rt.Out, "slug     %s\n", p.Slug)
	fmt.Fprintf(rt.Out, "name     %s\n", p.Name)
	fmt.Fprintf(rt.Out, "color    %s\n", p.Color)
	fmt.Fprintf(rt.Out, "state    %s\n", stateStr(p.Archived))
	acts, err := service.Activities(ctx, p.TeamID, p.ID, true)
	if err != nil {
		return fmt.Errorf("list activities for project %d: %w", p.ID, err)
	}
	if len(acts) > 0 {
		fmt.Fprintln(rt.Out, "activities:")
		for _, a := range acts {
			marker := ""
			if a.Archived {
				marker = " (archived)"
			}
			fmt.Fprintf(rt.Out, "  - %s%s\n", a.Name, marker)
		}
	}
	return nil
}

func runProjectRename(rt *Runtime, args []string) error {
	if len(args) != 2 {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack project rename <slug|id> <new-name>")}
	}
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	service := services.Projects
	id, err := mustProjectID(services.ProjectLookup, ctx, args[0])
	if err != nil {
		return err
	}
	p, err := services.ProjectLookup.GetProjectByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}
	callerID, err := projectActorID(services.Workspace, ctx, p.TeamID)
	if err != nil {
		return fmt.Errorf("resolve project workspace owner: %w", err)
	}
	upd, err := service.Update(ctx, appmodel.ProjectUpdateRequest{TeamID: p.TeamID, ProjectID: id, CallerID: callerID, Update: appmodel.ProjectUpdate{Name: args[1]}})
	if err != nil {
		return fmt.Errorf("rename project: %w", err)
	}
	fmt.Fprintf(rt.Out, "renamed #%d → %q\n", upd.ID, upd.Name)
	return nil
}

func runProjectColor(rt *Runtime, args []string) error {
	if len(args) != 2 {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack project color <slug|id> <#hex>")}
	}
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	service := services.Projects
	id, err := mustProjectID(services.ProjectLookup, ctx, args[0])
	if err != nil {
		return err
	}
	p, err := services.ProjectLookup.GetProjectByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}
	callerID, err := projectActorID(services.Workspace, ctx, p.TeamID)
	if err != nil {
		return fmt.Errorf("resolve project workspace owner: %w", err)
	}
	upd, err := service.Update(ctx, appmodel.ProjectUpdateRequest{TeamID: p.TeamID, ProjectID: id, CallerID: callerID, Update: appmodel.ProjectUpdate{Color: args[1]}})
	if err != nil {
		return fmt.Errorf("set project color: %w", err)
	}
	fmt.Fprintf(rt.Out, "color #%d → %s\n", upd.ID, upd.Color)
	return nil
}

func runProjectArchive(rt *Runtime, args []string, archive bool) error {
	if len(args) != 1 {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack project archive|unarchive <slug|id>")}
	}
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	service := services.Projects
	id, err := mustProjectID(services.ProjectLookup, ctx, args[0])
	if err != nil {
		return err
	}
	p, err := services.ProjectLookup.GetProjectByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}
	verb := "unarchived"
	if archive {
		verb = "archived"
	}
	callerID, err := projectActorID(services.Workspace, ctx, p.TeamID)
	if err != nil {
		return fmt.Errorf("resolve project workspace owner: %w", err)
	}
	if _, err := service.Update(ctx, appmodel.ProjectUpdateRequest{TeamID: p.TeamID, ProjectID: id, CallerID: callerID, Update: appmodel.ProjectUpdate{Archived: &archive}}); err != nil {
		return fmt.Errorf("set project archived state: %w", err)
	}
	fmt.Fprintf(rt.Out, "%s #%d\n", verb, id)
	return nil
}

func runProjectDelete(rt *Runtime, args []string) error {
	if len(args) != 1 {
		return &ExitError{Code: 2, Err: errors.New("usage: paratrack project delete <slug|id>")}
	}
	services, ctx, err := applicationForCommand(rt)
	if err != nil {
		return err
	}
	service := services.Projects
	id, err := mustProjectID(services.ProjectLookup, ctx, args[0])
	if err != nil {
		return err
	}
	p, err := services.ProjectLookup.GetProjectByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get project: %w", err)
	}
	callerID, err := projectActorID(services.Workspace, ctx, p.TeamID)
	if err != nil {
		return fmt.Errorf("resolve project workspace owner: %w", err)
	}
	if err := service.Delete(ctx, appmodel.ProjectMutationRequest{TeamID: p.TeamID, ProjectID: id, CallerID: callerID}); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return errors.New("project not found")
		}
		return fmt.Errorf("delete project: %w", err)
	}
	fmt.Fprintf(rt.Out, "deleted #%d (activities in it are now 'Uncategorized')\n", id)
	return nil
}

func stateStr(archived bool) string {
	if archived {
		return "archived"
	}
	return "active"
}
