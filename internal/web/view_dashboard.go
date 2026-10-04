package web

import (
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// teamsView is the minimal row the workspace switcher dropdown needs:
// team identity, role, and id (for the form post).
type teamsView struct {
	ID   int64
	Name string
	Role string
}

// sessionView is the per-row representation of an active or recent
// session in the dashboard / stats tables.
type sessionView struct {
	ID           int64
	ActivityID   int64
	PersonName   string // whose session (managers' team view)
	ActivityName string
	Color        string
	ProjectID    int64  // 0 if activity has no project
	ProjectName  string // empty if no project
	ProjectColor string // empty if no project
	ProjectSlug  string // empty if no project
	StartISO     string
	// ResumeISO anchors the live clock: the last resume, not the start.
	// Counting from the start double-counted every pause after a resume.
	ResumeISO          string
	Clock              string // H:MM:SS of the full tracked total (running list)
	StartLocal         string
	EndLocal           string
	StartInput         string // value for datetime-local
	EndInput           string
	Duration           string
	DurationSecs       int    // clipped tracked seconds behind Duration — aggregate from this, never parse the label
	DurationInput      string // user-editable representation ("1h 30m")
	AccumulatedSeconds int
	Paused             bool
	Note               string
	Tags               []tagChip // attached tags from the dashboard read snapshot
	Lang               string    // i18n for fragment templates (session-row, active-list)
}

func (sessionView) isTemplateData() {}

// T translates a dictionary key. Fragment templates call {{.T "key"}}.
func (v sessionView) T(key string) string { return i18n.T(i18n.Lang(v.Lang), key) }

// dashboardData feeds dashboard.html.
type dashboardData struct {
	pageData
	Activities     []activityView
	Projects       []projectView // for the project picker on the start form
	ActiveSessions []sessionView
	Recent         []sessionView
	ActiveCount    int
	// Running vs paused: "Активных 3" read wrong when two were paused.
	RunningCount int
	PausedCount  int
	TodayTotal   string
	TodaySecs    int // seed for the live-ticking «учтено»
	TopToday     string
	Goals        []goalView
	ActiveVM     activeListVM // wrapper so active-list can call {{.T}}
	GoalsVM      goalsListVM  // wrapper so goals-list can call {{.T}}

	HasProject     bool
	HasSession     bool
	Unbilled       []unbilledView // "not invoiced yet", when there is any
	DefaultProject int64          // user's project for new timers (prefs), 0 = remember the last
}
