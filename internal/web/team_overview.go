package web

import (
	"net/http"
	"sort"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// overviewView is the manager's read of the week on the statistics page (PLAN-05): each
// project's plan against what was tracked on it, and each person's planned load against the
// time they have for clients. Overload and idle follow the schedule's rules.
type overviewView struct {
	DayNorm  string
	Projects []overviewProject
	People   []overviewPerson
}

type overviewProject struct {
	Name string
	Plan string
	Fact string
	Over bool
}

type overviewPerson struct {
	Name      string
	Planned   string
	Available string
	Tracked   string
	Mark      string
}

// overviewOf builds the team's week: the plan from the schedule, the fact from the sessions.
func (s *Server) overviewOf(r *http.Request, now time.Time) (*overviewView, error) {
	lang := resolveLang(r)
	today := startOfDay(now)
	start := startOfWeek(r, today)
	days := make([]time.Time, 7)
	for i := range days {
		days[i] = start.AddDate(0, 0, i)
	}
	snapshot, err := s.services.Scheduling.List(r.Context(), appmodel.ScheduleQuery{TeamID: teamID(r), WeekStart: start, Now: now})
	if err != nil {
		return nil, err
	}
	fact, err := s.services.Scheduling.TrackedSecondsByProject(r.Context(), appmodel.PersonDayQuery{TeamID: teamID(r), Days: days, Now: now})
	if err != nil {
		return nil, err
	}
	view := &overviewView{DayNorm: fmtDur(r, dayNormSeconds), Projects: []overviewProject{}, People: []overviewPerson{}}
	planned := map[int64]int{}
	for _, row := range snapshot.Rows {
		for projectID, perDay := range row.ByProject {
			for _, minutes := range perDay {
				planned[projectID] += minutes
			}
		}
		view.People = append(view.People, overviewPerson{
			Name:      row.UserName,
			Planned:   fmtDur(r, row.Total*60),
			Available: fmtDur(r, row.AvailableMinutes*60),
			Tracked:   fmtDur(r, row.Tracked*60),
			Mark:      loadMark(lang, row.Total, row.AvailableMinutes),
		})
	}
	sort.SliceStable(view.People, func(i, j int) bool { return view.People[i].Name < view.People[j].Name })
	projectIDs := map[int64]bool{}
	for id := range planned {
		projectIDs[id] = true
	}
	for id := range fact {
		projectIDs[id] = true
	}
	for id := range projectIDs {
		name, ok := snapshot.ProjectNames[id]
		if !ok {
			continue
		}
		seconds := fact[id]
		view.Projects = append(view.Projects, overviewProject{
			Name: name, Plan: fmtDur(r, planned[id]*60), Fact: fmtDur(r, seconds),
			Over: planned[id] > 0 && seconds > planned[id]*60,
		})
	}
	sort.SliceStable(view.Projects, func(i, j int) bool { return view.Projects[i].Name < view.Projects[j].Name })
	return view, nil
}

// loadMark names a person's week as overloaded (more planned than the time available) or
// idle (less than half of it planned, or nothing at all) and says nothing in between.
func loadMark(lang i18n.Lang, planned, available int) string {
	switch {
	case available <= 0:
		return ""
	case planned > available:
		return i18n.T(lang, "overview.overload")
	case planned*2 < available:
		return i18n.T(lang, "overview.idle")
	}
	return ""
}
