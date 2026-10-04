package cli

import (
	"context"
	"fmt"
	"github.com/aa-blinov/paratrack/internal/appmodel"
	"text/tabwriter"

	"github.com/aa-blinov/paratrack/internal/cliport"
	"github.com/aa-blinov/paratrack/internal/requestctx"
)

func applicationForCommand(rt *Runtime) (*cliport.Services, context.Context, error) {
	services, err := rt.application()
	if err != nil {
		return nil, nil, fmt.Errorf("initialize application: %w", err)
	}
	ctx := rt.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return services, ctx, nil
}

func defaultTeamForCommand(services *cliport.Services, ctx context.Context) (int64, context.Context, error) {
	teamID, err := services.Workspace.DefaultTeam(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("resolve default workspace: %w", err)
	}
	if teamID <= 0 {
		return 0, nil, fmt.Errorf("no workspace available")
	}
	ownerID, err := services.Workspace.TeamOwnerID(ctx, teamID)
	if err != nil {
		return 0, nil, fmt.Errorf("resolve default workspace owner: %w", err)
	}
	if ownerID <= 0 {
		return 0, nil, fmt.Errorf("default workspace has no valid owner")
	}
	ctx = requestctx.WithActor(ctx, ownerID)
	ctx = requestctx.WithScope(ctx, ownerID)
	return teamID, ctx, nil
}

func shortDurSeconds(total int) string {
	if total < 0 {
		total = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, (total/60)%60, total%60)
}

// printActive renders the active-session table for both `status` and
// the bare `paratrack` invocation.
func printActive(rt *Runtime, rows []appmodel.ActiveSession) {
	if len(rows) == 0 {
		fmt.Fprintln(rt.Out, "No active sessions. Start one with: paratrack start <activity>")
		return
	}
	tw := tabwriter.NewWriter(rt.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ACTIVITY\tSTATUS\tSTARTED\tDURATION")
	fmt.Fprintln(tw, "--------\t------\t-------\t--------")
	now := rt.now()
	for _, as := range rows {
		status := "Active"
		if as.Session.Paused {
			status = "Paused"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			as.Activity.Name,
			status,
			as.Session.StartAt.In(now.Location()).Format("15:04:05"),
			shortDurSeconds(as.Session.DurationSeconds(now)),
		)
	}
	_ = tw.Flush()
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
