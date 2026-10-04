package appmodel

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
