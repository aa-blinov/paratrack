package appmodel

type UserPrefsSaveCommand struct {
	UserID int64
	JSON   string
}

type PreferencesSaveRequest struct {
	UserID      int64
	TeamID      int64
	Preferences UserPreferences
}
