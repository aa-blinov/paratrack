package appmodel

type UserPreferences struct {
	HiddenSections []string
	Tabs           []string
	Duration       string
	WeekStart      string
	TZ             string
	HiddenWidgets  []string
	DefaultProject map[string]int64
	// LastTeamID is the workspace this person worked in last. It is a
	// convenience for the next login, never a grant: membership is
	// re-checked on every request.
	LastTeamID int64
}
