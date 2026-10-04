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
	snapshot, err := s.services.Dashboard.BuildActiveList(ctx, team)
	if err != nil {
		return activeListVM{}, err
	}
	views := activeSessionViews(snapshot.ActiveSessions, today.Start, today.End, now, resolveLang(r), durFmtOf(r))
	attachSessionTags(views, snapshot.TagsBySession)
	attachSessionProjects(views, snapshot.ProjectsByID)
	vm := activeListVM{Lang: string(resolveLang(r)), Items: views, Running: countRunning(views)}
	vm.Projects, vm.FirstRun = projectViews(snapshot.Projects), snapshot.FirstRun
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
