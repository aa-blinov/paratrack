package web

import (
	"encoding/json"
	"fmt"
	"net/http"
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
	var people []personOpt
	names := map[int64]string{}
	scope, err := s.resolveReportPersonScope(r, true)
	if err != nil {
		return statsData{}, err
	}
	ctx, personFilter := scope.Context, scope.UserID
	if len(scope.Members) > 1 {
		for _, member := range scope.Members {
			name := member.Name
			if name == "" {
				name = member.Email
			}
			names[member.UserID] = name
			people = append(people, personOpt{ID: member.UserID, Name: name, Selected: member.UserID == personFilter})
		}
	}

	projectFilter := strings.TrimSpace(r.URL.Query().Get("project"))
	tagFilter := strings.TrimSpace(r.URL.Query().Get("tag"))
	stats, err := s.services.ReportBuilder.BuildStats(ctx, appmodel.ReportStatsQuery{
		TeamID: teamID(r), From: period.Start, To: period.End, Now: now,
		ProjectSlug: projectFilter, Tag: tagFilter,
		Uncategorized: i18n.T(resolveLang(r), "dash.uncategorized"),
	})
	if err != nil {
		return statsData{}, fmt.Errorf("build stats report: %w", err)
	}
	if stats.Project.ID == 0 {
		projectFilter = ""
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
	user, _ := UserFrom(ctx)
	query := r.URL.Query()
	query.Set("log", "all")
	return statsData{
		SessionsCut:   len(shown) < len(rows),
		ShowAllURL:    "/stats?" + query.Encode(),
		MeID:          user.ID,
		People:        people,
		PersonFilter:  personFilter,
		pageData:      pageData{Title: "Stats", Active: "stats"},
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
	}, nil
}

func (s *Server) buildGraphData(r *http.Request) (graphData, error) {
	now := userNow(r)
	period := s.parsePeriodAt(r, now)
	scope, err := s.resolveReportPersonScope(r, false)
	if err != nil {
		return graphData{}, err
	}
	ctx, personFilter, personName := scope.Context, scope.UserID, scope.Name

	projectFilter := strings.TrimSpace(r.URL.Query().Get("project"))
	tagFilter := strings.TrimSpace(r.URL.Query().Get("tag"))
	graph, err := s.services.ReportBuilder.BuildGraph(ctx, appmodel.ReportGraphQuery{
		TeamID: teamID(r), From: period.Start, To: period.End, Now: now,
		ProjectSlug: projectFilter, Tag: tagFilter,
	})
	if err != nil {
		return graphData{}, fmt.Errorf("build graph report: %w", err)
	}
	if graph.Project.ID == 0 {
		projectFilter = ""
	}
	chart := chartDataFromGraph(graph.Graph, period, resolveLang(r))
	chartJSON, err := json.Marshal(chart)
	if err != nil {
		return graphData{}, fmt.Errorf("encode graph chart: %w", err)
	}
	return graphData{
		pageData: pageData{Title: "Graph", Active: "graph"},
		Period:   period, Chart: chart, ChartJSON: string(chartJSON),
		ProjectFilter: projectFilter, ProjectName: graph.Project.Name,
		TagFilter: tagFilter, PersonFilter: personFilter, PersonName: personName,
	}, nil
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

	activities, projects := statsAggregateViews(r, stats.Summary)
	return rows, activities, projects
}

func statsAggregateViews(r *http.Request, summary appmodel.ReportStatsSummary) ([]aggRow, []projectAggRow) {
	activities := make([]aggRow, 0, len(summary.Activities))
	for _, activity := range summary.Activities {
		activities = append(activities, aggRow{
			ActivityName: activity.Name,
			Color:        colorFor(activity.Name),
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
				Color:        colorFor(activity.Name),
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
