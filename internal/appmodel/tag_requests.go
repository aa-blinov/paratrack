package appmodel

// SessionTagsQuery loads tag associations for a batch of sessions in one workspace.
// TeamID zero is reserved for legacy unscoped sessions.
type SessionTagsQuery struct {
	TeamID     int64
	SessionIDs []int64
}

type TagCreateRequest struct {
	TeamID   int64
	CallerID int64
	Name     string
}

type TagDeleteRequest struct {
	TeamID   int64
	CallerID int64
	TagID    int64
}

type SessionTagRequest struct {
	TeamID    int64
	CallerID  int64
	SessionID int64
	Name      string
}
