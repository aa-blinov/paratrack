package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

func (s *Server) buildStatsData(r *http.Request) (statsData, error) {
	now := userNow(r)
	period := s.parsePeriodAt(r, now)
	projectFilter := strings.TrimSpace(r.URL.Query().Get("project"))
	tagFilter := strings.TrimSpace(r.URL.Query().Get("tag"))
	stats, err := s.services.ReportBuilder.BuildStats(r.Context(), appmodel.ReportStatsQuery{
		TeamID: teamID(r), From: period.Start, To: period.End, Now: now,
		PersonID: requestedReportPerson(r), IncludePeople: canManage(r),
		ProjectSlug: projectFilter, Tag: tagFilter,
		Uncategorized: i18n.T(resolveLang(r), "dash.uncategorized"),
	})
	if err != nil {
		return statsData{}, fmt.Errorf("build stats report: %w", err)
	}
	if stats.Project.ID == 0 {
		projectFilter = ""
	}
	var people []personOpt
	names := make(map[int64]string, len(stats.People))
	for _, member := range stats.People {
		names[member.UserID] = member.Name
		if len(stats.People) > 1 {
			people = append(people, personOpt{ID: member.UserID, Name: member.Name, Selected: member.Selected})
		}
	}
	rows, activities, projects := s.statsPresentation(r, period, now, names, stats)
	shown := rows
	logCap := statsLogRows
	if r.URL.Query().Get("log") == "all" {
		logCap = statsLogAll
	}
	if len(shown) > logCap {
		shown = shown[:logCap]
	}
	savedReports, err := s.loadSavedReports(r)
	if err != nil {
		return statsData{}, fmt.Errorf("load saved reports for stats: %w", err)
	}
	user, _ := UserFrom(r.Context())
	query := r.URL.Query()
	query.Set("log", "all")
	data := statsData{
		SessionsCut:   len(shown) < len(rows),
		ShowAllURL:    "/stats?" + query.Encode(),
		MeID:          user.ID,
		People:        people,
		PersonFilter:  stats.PersonFilter,
		pageData:      pageData{Title: "Stats", Active: "stats", Lang: string(resolveLang(r)), ReactApp: true},
		Period:        period,
		Aggregated:    activities,
		ByProject:     projects,
		Projects:      projectViews(stats.Projects),
		ProjectFilter: projectFilter,
		Sessions:      shown,
		Total:         fmtDur(r, stats.Summary.TotalSeconds),
		SessionCount:  len(rows),
		TagFilter:     tagFilter,
		AllTagNames:   statsTagNames(stats.Tags),
		SavedReports:  savedReportViews(savedReports),
	}
	// "Сегодня" is the default period and matches the dashboard's own
	// "учтено сегодня" window, so an empty today is a real answer. It still
	// reads as "nothing was ever tracked" unless the page names the periods
	// that do hold time, so the empty view pays for that one extra read.
	if len(rows) == 0 {
		elsewhere, err := s.statsPeriodsWithTime(r, now, projectFilter, tagFilter, period.Label)
		if err != nil {
			// The hint is an addition to an honest empty state, so a failed
			// read must not replace the page with an error.
			s.logInternalError(err)
		} else {
			data.Elsewhere = elsewhere
		}
	}
	return data, nil
}

// statsNavPeriods are the periods the /stats switcher offers, in the order
// the switcher shows them.
var statsNavPeriods = []string{"today", "yesterday", "week", "last_week", "month", "last_month"}

// statsPeriodsWithTime reports which switcher periods hold tracked time when
// the selected one holds none, so an empty /stats period does not read as
// "nothing was tracked". Windows come from parsePeriodAt, so a Sunday-based
// week preference moves them exactly as it moves the links the hint leads to.
func (s *Server) statsPeriodsWithTime(r *http.Request, now time.Time, projectSlug, tag, current string) ([]statsPeriodOption, error) {
	// "last_month" starts before every other switcher window and ends at
	// "now" for all of them, so one read covers the whole switcher.
	widest, err := timeparse.ResolvePeriod("last_month", now)
	if err != nil {
		return nil, fmt.Errorf("resolve widest stats period: %w", err)
	}
	stats, err := s.services.ReportBuilder.BuildStats(r.Context(), appmodel.ReportStatsQuery{
		TeamID: teamID(r), From: widest.Start, To: now, Now: now,
		PersonID: requestedReportPerson(r), IncludePeople: canManage(r),
		ProjectSlug: projectSlug, Tag: tag,
		Uncategorized: i18n.T(resolveLang(r), "dash.uncategorized"),
	})
	if err != nil {
		return nil, fmt.Errorf("build stats periods with time: %w", err)
	}
	options := make([]statsPeriodOption, 0, len(statsNavPeriods))
	for _, label := range statsNavPeriods {
		if label == current {
			continue
		}
		window := s.periodWindow(r, now, label)
		count, seconds := 0, 0
		for _, active := range stats.Sessions {
			if secs := active.Session.TrackedSecondsInWindow(window.Start, window.End, now); secs > 0 {
				count++
				seconds += secs
			}
		}
		if count > 0 {
			options = append(options, statsPeriodOption{Label: label, Count: count, Total: fmtDur(r, seconds)})
		}
	}
	return options, nil
}

// periodWindow resolves one named period against the request's week-start
// preference by replaying it through the same parser the page itself uses.
func (s *Server) periodWindow(r *http.Request, now time.Time, label string) timeparse.Period {
	probe := r.Clone(r.Context())
	query := probe.URL.Query()
	query.Set("period", label)
	probe.URL.RawQuery = query.Encode()
	return s.parsePeriodAt(probe, now)
}

func (s *Server) buildGraphData(r *http.Request) (graphData, error) {
	now := userNow(r)
	period := s.parsePeriodAt(r, now)
	projectFilter := strings.TrimSpace(r.URL.Query().Get("project"))
	tagFilter := strings.TrimSpace(r.URL.Query().Get("tag"))
	graph, err := s.services.ReportBuilder.BuildGraph(r.Context(), appmodel.ReportGraphQuery{
		TeamID: teamID(r), From: period.Start, To: period.End, Now: now,
		PersonID:    requestedReportPerson(r),
		ProjectSlug: projectFilter, Tag: tagFilter,
	})
	if err != nil {
		return graphData{}, fmt.Errorf("build graph report: %w", err)
	}
	if graph.Project.ID == 0 {
		projectFilter = ""
	}
	chart := chartDataFromGraph(graph.Graph, period, resolveLang(r), s.activityColors(r))
	chartJSON, err := json.Marshal(chart)
	if err != nil {
		return graphData{}, fmt.Errorf("encode graph chart: %w", err)
	}
	return graphData{
		pageData:   pageData{Title: "Graph", Active: "graph", ReactApp: true},
		GraphReact: true, Period: period,
		PeriodStartInput: period.Start.Format("2006-01-02T15:04"), PeriodEndInput: period.End.Format("2006-01-02T15:04"),
		Chart: chart, ChartJSON: string(chartJSON),
		ProjectFilter: projectFilter, ProjectName: graph.Project.Name,
		TagFilter: tagFilter, PersonFilter: graph.PersonFilter, PersonName: graph.PersonName,
	}, nil
}

func requestedReportPerson(r *http.Request) int64 {
	if !canManage(r) {
		return 0
	}
	id, _ := strconv.ParseInt(r.URL.Query().Get("person"), 10, 64)
	return id
}

// statsPresentation converts the report workflow result into transport view
// models. Aggregation, authorization scope, and persistence remain outside
// this presentation function.
func (s *Server) statsPresentation(
	r *http.Request,
	period timeparse.Period,
	now time.Time,
	personNames map[int64]string,
	stats appmodel.ReportStatsResult,
) ([]sessionView, []aggRow, []projectAggRow) {
	rows := make([]sessionView, 0, len(stats.Sessions))
	for _, active := range stats.Sessions {
		row := toSessionView(active.Session, active.Activity, period.Start, period.End, now, resolveLang(r), durFmtOf(r))
		if active.Session.UserID > 0 {
			row.PersonName = personNames[active.Session.UserID]
		}
		for _, tag := range stats.TagsBySession[row.ID] {
			row.Tags = append(row.Tags, tagChip{ID: tag.ID, Name: tag.Name})
		}
		rows = append(rows, row)
	}
	attachSessionProjects(rows, stats.ProjectsByID)

	activities, projects := statsAggregateViews(r, s.activityColors(r), stats.Summary)
	return rows, activities, projects
}

func statsAggregateViews(r *http.Request, colors map[string]string, summary appmodel.ReportStatsSummary) ([]aggRow, []projectAggRow) {
	activities := make([]aggRow, 0, len(summary.Activities))
	for _, activity := range summary.Activities {
		activities = append(activities, aggRow{
			ActivityName: activity.Name,
			Color:        activityColor(colors, activity.Name),
			Duration:     fmtDur(r, activity.Seconds),
			Share:        activity.Share,
		})
	}
	projects := make([]projectAggRow, 0, len(summary.Projects))
	for _, project := range summary.Projects {
		row := projectAggRow{
			ProjectID: project.ID, ProjectName: project.Name, Slug: project.Slug,
			Color: project.Color, Duration: fmtDur(r, project.Seconds), Share: project.Share,
			Activities: make([]aggRow, 0, len(project.Activities)),
		}
		for _, activity := range project.Activities {
			row.Activities = append(row.Activities, aggRow{
				ActivityName: activity.Name,
				Color:        activityColor(colors, activity.Name),
				Duration:     fmtDur(r, activity.Seconds),
				Share:        activity.Share,
			})
		}
		projects = append(projects, row)
	}
	return activities, projects
}

func statsTagNames(tags []model.Tag) []string {
	names := make([]string, len(tags))
	for i, tag := range tags {
		names[i] = tag.Name
	}
	return names
}
