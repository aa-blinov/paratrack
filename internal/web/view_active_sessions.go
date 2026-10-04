package web

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/timeparse"
)

func (s *Server) buildActiveListVM(r *http.Request) (activeListVM, error) {
	ctx := r.Context()
	team := teamID(r)
	now := userNow(r)
	today, err := timeparse.ResolvePeriod("today", now)
	if err != nil {
		return activeListVM{}, fmt.Errorf("resolve active-list day period: %w", err)
	}
	active, err := s.services.Tracking.Queries.ActiveSessions(ctx, team)
	if err != nil {
		return activeListVM{}, err
	}
	views := activeSessionViews(active, today.Start, today.End, now, resolveLang(r), durFmtOf(r))
	hydrateSessionTags(ctx, s.services.SessionTags, team, views, s.logger)
	hydrateSessionProjects(ctx, s.services.Projects.Queries, team, views, s.logger)
	vm := activeListVM{Lang: string(resolveLang(r)), Items: views, Running: countRunning(views)}
	projects, err := s.services.Projects.Queries.List(ctx, team, false)
	if err != nil {
		return activeListVM{}, err
	}
	vm.Projects = projectViews(projects)
	if len(views) == 0 {
		seen, err := s.services.Tracking.Queries.HasAnySession(ctx, team)
		if err != nil {
			return activeListVM{}, err
		}
		vm.FirstRun = !seen
	}
	return vm, nil
}

func (s *Server) buildMiniBarVM(r *http.Request) (activeListVM, error) {
	ctx := r.Context()
	now := userNow(r)
	today, err := timeparse.ResolvePeriod("today", now)
	if err != nil {
		return activeListVM{}, fmt.Errorf("resolve mini-bar day period: %w", err)
	}
	active, err := s.services.Tracking.Queries.ActiveSessions(ctx, teamID(r))
	if err != nil {
		return activeListVM{}, err
	}
	lang := resolveLang(r)
	views := activeSessionViews(active, today.Start, today.End, now, lang, durFmtOf(r))
	// Running first: the bar shows the timer that is actually ticking.
	sort.SliceStable(views, func(i, j int) bool { return !views[i].Paused && views[j].Paused })
	return activeListVM{Lang: string(lang), Items: views}, nil
}

func activeSessionViews(active []model.ActiveSession, periodStart, periodEnd, now time.Time, lang i18n.Lang, durationFormat string) []sessionView {
	views := make([]sessionView, 0, len(active))
	for _, item := range active {
		views = append(views, toSessionView(item.Session, item.Activity, periodStart, periodEnd, now, lang, durationFormat))
	}
	return views
}
