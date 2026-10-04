package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/money"
)

// buildDashboardData adapts the dashboard workflow snapshot into template
// view models. Request preferences and query-string hints stay at the HTTP
// boundary; aggregation data remains owned by the dashboard workflow.
func buildDashboardData(r *http.Request, now time.Time, snapshot appmodel.DashboardSnapshot) (dashboardData, error) {
	langValue := resolveLang(r)
	lang := string(langValue)
	durFmt := durFmtOf(r)
	activeViews := make([]sessionView, 0, len(snapshot.ActiveSessions))
	for _, session := range snapshot.ActiveSessions {
		activeViews = append(activeViews, toSessionView(session.Session, session.Activity, snapshot.TodayStart, snapshot.TodayEnd, now, langValue, durFmt))
	}
	todayViews := make([]sessionView, 0, len(snapshot.TodaySessions))
	for _, session := range snapshot.TodaySessions {
		todayViews = append(todayViews, toSessionView(session.Session, session.Activity, snapshot.TodayStart, snapshot.TodayEnd, now, langValue, durFmt))
	}
	recentViews := make([]sessionView, 0, len(snapshot.RecentSessions))
	for _, session := range snapshot.RecentSessions {
		recentViews = append(recentViews, toSessionView(session.Session, session.Activity, snapshot.RecentStart, snapshot.RecentEnd, now, langValue, durFmt))
	}
	for _, views := range [][]sessionView{activeViews, todayViews, recentViews} {
		attachSessionTags(views, snapshot.TagsBySession)
		attachSessionProjects(views, snapshot.ProjectsByID)
	}
	if len(recentViews) > 8 {
		recentViews = recentViews[:8]
	}

	runningCount := countRunning(activeViews)
	projects := snapshot.Projects
	data := dashboardData{
		pageData:       pageData{Title: "Dashboard", Active: "dashboard", Lang: lang},
		Activities:     activityViews(snapshot.Activities, lang),
		Projects:       projectViews(projects),
		ActiveSessions: activeViews,
		Recent:         recentViews,
		ActiveCount:    len(activeViews),
		RunningCount:   runningCount,
		PausedCount:    len(activeViews) - runningCount,
		ActiveVM:       activeListVM{Lang: lang, Items: activeViews, Running: runningCount},
		HasProject:     len(projects) > 0,
		HasSession:     snapshot.HasSession,
	}
	if defaultID := defaultProject(prefsOf(r), teamID(r)); defaultID > 0 {
		for _, project := range projects {
			if project.ID == defaultID {
				data.DefaultProject = defaultID
				break
			}
		}
	}
	// Project links can preselect a timer's project only when it belongs to
	// the already scoped workspace snapshot.
	if requestedID, err := strconv.ParseInt(r.URL.Query().Get("project"), 10, 64); err == nil && requestedID > 0 {
		for _, project := range projects {
			if project.ID == requestedID {
				data.DefaultProject = requestedID
				break
			}
		}
	}
	data.ActiveVM.Projects = projectViews(projects)
	data.ActiveVM.FirstRun = !data.HasSession
	if len(snapshot.Unbilled) > 0 {
		data.Unbilled = unbilledViewsFrom(snapshot.Unbilled, r)
	}

	activityTotals := make(map[string]int)
	total := 0
	for _, session := range append(todayViews, activeViews...) {
		updated, err := money.AddInt(activityTotals[session.ActivityName], session.DurationSecs)
		if err != nil {
			return dashboardData{}, fmt.Errorf("sum dashboard activity time: %w", err)
		}
		activityTotals[session.ActivityName] = updated
		total, err = money.AddInt(total, session.DurationSecs)
		if err != nil {
			return dashboardData{}, fmt.Errorf("sum dashboard time: %w", err)
		}
	}
	data.TodayTotal = fmtDur(r, total)
	data.TodaySecs = total
	topName, topSeconds := "", 0
	for name, seconds := range activityTotals {
		if seconds > topSeconds {
			topName, topSeconds = name, seconds
		}
	}
	data.TopToday = shortSummary(topName)

	// Goal progress is optional dashboard decoration; failed goal reads have
	// already been represented by an empty Goals slice in the workflow result.
	data.Goals = toGoalViews(snapshot.Goals, langValue)
	for i := range data.Goals {
		data.Goals[i].Lang = lang
		data.Goals[i].PeriodRangeLabel = periodRangeLabel(data.Goals[i].Period, langValue)
	}
	data.GoalsVM = goalsListVM{Lang: lang, Goals: data.Goals}
	return data, nil
}

// shortSummary joins names with a comma for the dashboard's "Top today" line.
func shortSummary(name string) string {
	if name == "" {
		return ""
	}
	return strings.TrimSpace(name)
}
