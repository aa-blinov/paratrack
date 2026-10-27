package web

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// projectPlanView is what the project page shows about the team's time on
// it: the plan, the tracked time, what is left of the estimate, and the people.
type projectPlanView struct {
	People       []projectPersonRow
	PlannedLabel string
	// TrackedLabel is the team's tracked time on the project, for those who see it.
	TrackedLabel   string
	RemainingLabel string
	RemainingOver  bool
	// LeadLabel names the project lead, or is empty when there is none.
	LeadLabel string
	// Weeks are the weeks around today, and the norm in clock form when there is one (PLAN-02).
	Weeks           []projectWeekRow
	WeeklyNormLabel string
	WeeklyNormInput string
}

// projectWeekRow is one week of the project: the plan, the tracked time when the
// viewer may see it, and how far the week strays from the weekly norm.
type projectWeekRow struct {
	Start     string
	Label     string
	Planned   string
	Tracked   string
	Deviation string
	Off       bool
	Current   bool
}

// projectPlan builds the plan view. The tracked column and the left-of-estimate
// figure are for managers and the project's own lead; everyone sees the plan.
func (s *Server) projectPlan(r *http.Request, project model.Project) (projectPlanView, error) {
	now := userNow(r)
	planned, tracked, err := s.services.Scheduling.ProjectPlanByMember(r.Context(), teamID(r), project.ID, now)
	if err != nil {
		return projectPlanView{}, err
	}
	ahead, err := s.services.Scheduling.PlannedAhead(r.Context(), teamID(r), project.ID, startOfDay(now))
	if err != nil {
		return projectPlanView{}, err
	}
	members, err := s.services.Teams.Directory.Members(r.Context(), teamID(r))
	if err != nil {
		return projectPlanView{}, err
	}
	lang := resolveLang(r)
	viewer := authenticatedUserID(r)
	showTracked := canManage(r) || (project.LeadUserID > 0 && project.LeadUserID == viewer)
	view := projectPlanView{People: []projectPersonRow{}}
	var plannedTotal, trackedTotal int
	names := make(map[int64]string, len(members))
	for _, member := range members {
		name := member.Name
		if name == "" {
			name = member.Email
		}
		names[member.UserID] = name
		if project.LeadUserID == member.UserID {
			view.LeadLabel = name
		}
	}
	for _, member := range members {
		trackedTotal += tracked[member.UserID]
		plannedTotal += planned[member.UserID]
		if planned[member.UserID] == 0 && (!showTracked || tracked[member.UserID] == 0) {
			continue
		}
		row := projectPersonRow{Name: names[member.UserID], Planned: fmtDur(r, planned[member.UserID]*60)}
		if showTracked {
			row.Tracked = fmtDur(r, tracked[member.UserID]*60)
		}
		view.People = append(view.People, row)
	}
	sort.Slice(view.People, func(i, j int) bool { return view.People[i].Name < view.People[j].Name })
	weekStarts := projectWeeks(now, 3, 2)
	weeks, err := s.services.Scheduling.ProjectWeekTotals(r.Context(), teamID(r), project.ID, weekStarts, now)
	if err != nil {
		return projectPlanView{}, err
	}
	norm, hasNorm, err := s.services.Scheduling.ProjectWeeklyNorm(r.Context(), teamID(r), project.ID)
	if err != nil {
		return projectPlanView{}, err
	}
	for i, week := range weekStarts {
		view.Weeks = append(view.Weeks, projectWeekRowOf(r, week, weeks[i], norm*60, hasNorm, now, showTracked))
	}
	if hasNorm {
		view.WeeklyNormLabel = fmtDur(r, norm*60)
		view.WeeklyNormInput = fmtDurF(lang, "clock", norm*60)
	}
	view.PlannedLabel = fmtDur(r, plannedTotal*60)
	if showTracked {
		view.TrackedLabel = fmtDur(r, trackedTotal*60)
	}
	if project.EstimateMinutes != nil && showTracked {
		left := *project.EstimateMinutes - trackedTotal - ahead
		if left < 0 {
			view.RemainingOver = true
			view.RemainingLabel = fmt.Sprintf(i18n.T(lang, "proj.overBy"), fmtDur(r, -left*60))
		} else {
			view.RemainingLabel = fmtDur(r, left*60)
		}
	}
	return view, nil
}

// deadlineNote says how far off the deadline is, and whether it has passed.
func deadlineNote(r *http.Request, deadline string) (note string, overdue bool) {
	if deadline == "" {
		return "", false
	}
	day, err := time.ParseInLocation("2006-01-02", deadline, userLoc(r))
	if err != nil {
		return "", false
	}
	days := int(day.Sub(startOfDay(userNow(r))).Hours() / 24)
	lang := resolveLang(r)
	if days < 0 {
		return i18n.T(lang, "proj.deadlineOver"), true
	}
	return fmt.Sprintf(i18n.T(lang, "proj.deadlineLeft"), days), false
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// deadlineLabel is the deadline as a day in the reader's language, or "".
func deadlineLabel(r *http.Request, deadline string) string {
	if deadline == "" {
		return ""
	}
	day, err := time.ParseInLocation("2006-01-02", deadline, userLoc(r))
	if err != nil {
		return ""
	}
	return fmtDate(resolveLang(r), day)
}

// projectWeeks lists the weeks around now by their first day (Monday), from back
// weeks before this one to ahead weeks after it.
func projectWeeks(now time.Time, back, ahead int) []time.Time {
	monday := startOfDay(now).AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))
	weeks := make([]time.Time, 0, back+ahead+1)
	for i := -back; i <= ahead; i++ {
		weeks = append(weeks, monday.AddDate(0, 0, 7*i))
	}
	return weeks
}

// projectWeekRowOf prints one week. A week that is over is judged by the time
// tracked in it, but only where the viewer may see that time: anyone else is
// judged by the plan, so the norm does not leak the tracked figures (PLAN-02).
func projectWeekRowOf(r *http.Request, week time.Time, total appmodel.ProjectWeekTotal, normSeconds int, hasNorm bool, now time.Time, showTracked bool) projectWeekRow {
	lang := resolveLang(r)
	nextWeek := week.AddDate(0, 0, 7)
	row := projectWeekRow{
		Start:   week.Format("2006-01-02"),
		Label:   periodSpan(lang, fmtDay(lang, week), fmtDay(lang, nextWeek.AddDate(0, 0, -1))),
		Planned: fmtDur(r, total.PlannedMinutes*60),
		Current: !now.Before(week) && now.Before(nextWeek),
	}
	if showTracked {
		row.Tracked = fmtDur(r, total.TrackedSeconds)
	}
	if hasNorm {
		figure := total.PlannedMinutes * 60
		if showTracked && !now.Before(nextWeek) {
			figure = total.TrackedSeconds
		}
		row.Off = weekOff(figure, normSeconds)
		row.Deviation = deviationLabel(r, figure-normSeconds)
	}
	return row
}

// weekOff reports whether a week strays from the norm by more than a tenth of it.
// A week a little short is normal; a week far off in either direction is not.
func weekOff(figure, norm int) bool {
	diff := figure - norm
	if diff < 0 {
		diff = -diff
	}
	return norm > 0 && diff*10 > norm
}

// deviationLabel signs a difference from the norm: "+4:00" over, "-4:00" under.
func deviationLabel(r *http.Request, diffSeconds int) string {
	sign := "+"
	if diffSeconds < 0 {
		sign = "-"
		diffSeconds = -diffSeconds
	}
	return sign + fmtDur(r, diffSeconds)
}
