package appmodel

import "errors"

// ErrTagNameTaken rejects a rename that would land on a name another tag in
// the same workspace already owns. Merging the two labels would silently drop
// one of them from every screen that filters by name, so the caller must
// choose a different name instead.
var ErrTagNameTaken = errors.New("tag name is already taken")

// SessionTagsQuery loads tag associations for a batch of sessions in one workspace.
// TeamID zero is reserved for legacy unscoped sessions.
type SessionTagsQuery struct {
	TeamID     int64
	SessionIDs []int64
}

// TagListQuery scopes workspace tag catalogs and optional usage counts.
type TagListQuery struct {
	TeamID int64
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

// TagRenameRequest relabels a workspace tag in place. Only the name changes —
// the tag keeps its identity, so sessions already carrying it keep carrying it.
type TagRenameRequest struct {
	TeamID   int64
	CallerID int64
	TagID    int64
	Name     string
}

type SessionTagRequest struct {
	TeamID    int64
	CallerID  int64
	SessionID int64
	Name      string
}
