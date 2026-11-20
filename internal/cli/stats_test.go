package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/cliport"
	"github.com/aa-blinov/paratrack/internal/model"
)

type statsSessionHistory struct{ closed []appmodel.ActiveSession }

func (s statsSessionHistory) ClosedSessions(context.Context, appmodel.ClosedSessionsQuery) ([]appmodel.ActiveSession, error) {
	return s.closed, nil
}

type statsTimerQueries struct{ active []appmodel.ActiveSession }

func (s statsTimerQueries) ActiveSessions(context.Context, int64) ([]appmodel.ActiveSession, error) {
	return s.active, nil
}

// `paratrack stats` counts what the web stats count: the day includes the
// timer that is still running, so the CLI and the dashboard agree.
func TestRunStatsCountsRunningTimers(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	closedAt := func(at time.Time) *time.Time { return &at }
	closed := appmodel.ActiveSession{
		Session:  model.Session{StartAt: now.Add(-3 * time.Hour), EndAt: closedAt(now.Add(-2 * time.Hour)), AccumulatedSeconds: 3600},
		Activity: model.Activity{Name: "Вёрстка"},
	}
	running := appmodel.ActiveSession{
		Session:  model.Session{StartAt: now.Add(-30 * time.Minute), LastResumeAt: closedAt(now.Add(-30 * time.Minute)), AccumulatedSeconds: 0},
		Activity: model.Activity{Name: "Созвон"},
	}

	out := &bytes.Buffer{}
	runtime := NewRuntime(strings.NewReader(""), out, io.Discard)
	runtime.Now = func() time.Time { return now }
	runtime.ServiceLoader = func(context.Context) (*cliport.Services, io.Closer, error) {
		return &cliport.Services{
			TimerQueries:    statsTimerQueries{active: []appmodel.ActiveSession{running}},
			TimerCommands:   &runtimeTimerCommands{},
			TimerOperations: &runtimeTimerOperations{},
			ActivityCatalog: &runtimeActivityCatalog{},
			SessionHistory:  statsSessionHistory{closed: []appmodel.ActiveSession{closed}},
			Workspace:       runtimeWorkspace{},
			ProjectLookup:   runtimeProjectLookup{},
			ProjectQueries:  runtimeProjectQueries{},
			ProjectCommands: runtimeProjectCommands{},
			TagQueries:      &runtimeTagQueries{},
			TagCommands:     &runtimeTagCommands{},
			GoalQueries:     &runtimeGoalQueries{},
			GoalCommands:    &runtimeGoalCommands{},
		}, nil, nil
	}

	if err := RunWithRuntime([]string{"stats", "--period", "today"}, runtime); err != nil {
		t.Fatalf("stats: %v", err)
	}
	table := out.String()
	if !strings.Contains(table, "Созвон") {
		t.Fatalf("the running timer is missing from the stats table:\n%s", table)
	}
	if !strings.Contains(table, "01:30:00") {
		t.Fatalf("the day total does not count the running half hour:\n%s", table)
	}
}
