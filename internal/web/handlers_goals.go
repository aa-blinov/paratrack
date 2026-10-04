package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// ---------- Goals ---------------------------------------------------

// handleGoals serves the /goals management page.
func (s *Server) handleGoals(w http.ResponseWriter, r *http.Request) {
	now := userNow(r)

	acts, err := s.services.Goals.Activities(r.Context(), teamID(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}

	progress, err := s.services.Goals.Progress(r.Context(), teamID(r), now)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}

	gv := toGoalViews(progress, resolveLang(r))
	lang := string(resolveLang(r))
	for i := range gv {
		gv[i].Lang = lang
		gv[i].PeriodRangeLabel = periodRangeLabel(gv[i].Period, i18n.Lang(lang))
	}
	data := struct {
		pageData
		Activities []activityView
		Goals      []goalView
		GoalsVM    goalsListVM
	}{
		pageData:   pageData{Title: "Goals", Active: "goals", Lang: lang},
		Activities: activityViews(acts, lang),
		Goals:      gv,
		GoalsVM:    goalsListVM{Lang: lang, Goals: gv},
	}
	s.render(w, r, "goals-content", &data)
}

// handleGoalsList returns all configured goals as JSON (no progress).
func (s *Server) handleGoalsList(w http.ResponseWriter, r *http.Request) {
	goals, err := s.services.Goals.List(r.Context(), teamID(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	if goals == nil {
		goals = []model.Goal{}
	}
	s.writeJSON(w, goalsListResponse{Goals: goalsFor(goals)})
}

// handleGoalsProgress returns goal + current-period progress for each
// configured goal. Powers the dashboard widget.
//
// When the request comes from HTMX (HX-Request header), the response is
// the rendered `goals-list` fragment so it can be swapped into the
// page directly. Plain GET returns JSON for tooling / scripts.
func (s *Server) handleGoalsProgress(w http.ResponseWriter, r *http.Request) {
	progress, err := s.services.Goals.Progress(r.Context(), teamID(r), userNow(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	views := toGoalViews(progress, resolveLang(r))
	glang := string(resolveLang(r))
	for i := range views {
		views[i].Lang = glang
		views[i].PeriodRangeLabel = periodRangeLabel(views[i].Period, i18n.Lang(glang))
	}
	if isHTMX(r) {
		s.renderFragment(w, "goals-list", goalsListVM{Lang: glang, Goals: views, CanManage: canManage(r)})
		return
	}
	if progress == nil {
		progress = []model.GoalProgress{}
	}
	s.writeJSON(w, goalProgressListResponse{Progress: goalProgressFor(progress)})
}

// handleGoalsUpsert creates or replaces a goal. Body params:
//
//	activity   (required)
//	period     required — daily | weekly | monthly
//	minutes    required — integer target in minutes
//
// HTMX callers get the refreshed `goals-list` fragment; plain requests
// keep the JSON shape.
func (s *Server) handleGoalsUpsert(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	activityName := strings.TrimSpace(r.FormValue("activity"))
	period := strings.TrimSpace(r.FormValue("period"))
	minsStr := strings.TrimSpace(r.FormValue("minutes"))
	if activityName == "" || period == "" || minsStr == "" {
		http.Error(w, "activity, period and minutes are required", 400)
		return
	}
	mins, err := strconv.Atoi(minsStr)
	if err != nil {
		http.Error(w, "minutes must be a positive integer", 400)
		return
	}
	g, err := s.services.Goals.UpsertForManager(r.Context(), appmodel.GoalUpsertRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), ActivityName: activityName, Period: period, Minutes: mins})
	if err != nil {
		if isGoalValidationError(err) {
			http.Error(w, err.Error(), 400)
		} else {
			s.writeInternalError(w, err)
		}
		return
	}
	s.toastL(w, r, "toast.saved", activityName, "success")
	if isHTMX(r) {
		s.respondGoalsList(w, r)
		return
	}
	s.writeJSON(w, goalFor(g))
}

// handleGoalsDelete removes a goal. Query params: activity + period.
func (s *Server) handleGoalsDelete(w http.ResponseWriter, r *http.Request) {
	activityName := strings.TrimSpace(r.URL.Query().Get("activity"))
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if activityName == "" || period == "" {
		http.Error(w, "activity and period query params are required", 400)
		return
	}
	if err := s.services.Goals.DeleteForManager(r.Context(), appmodel.GoalDeleteRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), ActivityName: activityName, Period: period}); err != nil {
		if isGoalValidationError(err) {
			http.Error(w, err.Error(), 400)
			return
		}
		if errors.Is(err, model.ErrNotFound) {
			http.Error(w, "activity not found", 404)
			return
		}
		if errors.Is(err, model.ErrGoalNotFound) {
			http.Error(w, "goal not found", 404)
			return
		}
		s.writeInternalError(w, err)
		return
	}
	s.toastL(w, r, "toast.deleted", activityName, "success")
	if isHTMX(r) {
		s.respondGoalsList(w, r)
		return
	}
	w.WriteHeader(200)
}

func isGoalValidationError(err error) bool {
	return errors.Is(err, appmodel.ErrInvalidGoalTeam) ||
		errors.Is(err, appmodel.ErrInvalidGoalActivity) ||
		errors.Is(err, appmodel.ErrInvalidGoalPeriod) ||
		errors.Is(err, appmodel.ErrInvalidGoalTarget)
}

// respondGoalsList renders the `goals-list` fragment for HTMX swaps.
func (s *Server) respondGoalsList(w http.ResponseWriter, r *http.Request) {
	progress, err := s.services.Goals.Progress(r.Context(), teamID(r), userNow(r))
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	gv := toGoalViews(progress, resolveLang(r))
	lang := string(resolveLang(r))
	for i := range gv {
		gv[i].Lang = lang
		gv[i].PeriodRangeLabel = periodRangeLabel(gv[i].Period, i18n.Lang(lang))
	}
	s.renderFragment(w, "goals-list", goalsListVM{Lang: string(resolveLang(r)), Goals: gv, CanManage: canManage(r)})
}

// toGoalViews renders goal progress as the view-models used by
// dashboard and /goals pages.
func toGoalViews(progress []model.GoalProgress, lang i18n.Lang) []goalView {
	out := make([]goalView, 0, len(progress))
	for _, p := range progress {
		// Lang is stamped by the caller after this returns.
		class := ""
		switch {
		case p.PercentComplete >= 100:
			class = "exceeded"
		case p.PercentComplete >= 80:
			class = "met"
		}
		out = append(out, goalView{
			ID:               p.Goal.ID,
			ActivityName:     p.ActivityName,
			Color:            colorFor(p.ActivityName),
			Period:           p.Goal.Period,
			TargetMinutes:    p.Goal.TargetMinutes,
			TargetLabel:      fmtMinutesL(lang, p.Goal.TargetMinutes),
			AchievedMinutes:  p.AchievedMinutes,
			AchievedLabel:    fmtMinutesL(lang, p.AchievedMinutes),
			Percent:          p.PercentComplete,
			AchievedClass:    class,
			PeriodStartLabel: fmtDay(lang, p.PeriodStart),
			PeriodEndLabel:   fmtDay(lang, p.PeriodEnd),
			PeriodRangeLabel: periodRangeLabel(p.Goal.Period, i18n.En), // caller re-stamps with page lang
		})
	}
	return out
}

// formatMinutes renders an integer minute count as a short label.
// periodRangeLabel returns a short human label for the goal period.
func periodRangeLabel(period string, lang i18n.Lang) string {
	switch period {
	case "daily":
		return i18n.T(lang, "period.today")
	case "weekly":
		return i18n.T(lang, "period.thisWeek")
	case "monthly":
		return i18n.T(lang, "period.thisMonth")
	}
	return period
}

type exportPage struct {
	pageData
	ReportsEnabled bool
}

// countRunning counts the timers that are ticking (not paused).
