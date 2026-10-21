package web

import (
	"fmt"
	"net/http"
	"sort"
	"time"

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
