package db

import "time"

// These request values are confined to the package-private compatibility
// paths below. Application workflows cannot invoke unscoped database writes.
type legacyActivityRequest struct {
	TeamID int64
	Name   string
}

type legacyTagCreateRequest struct {
	TeamID int64
	Name   string
}

type legacyTagDeleteRequest struct {
	TeamID int64
	TagID  int64
}

type legacySessionTagRequest struct {
	TeamID    int64
	SessionID int64
	Name      string
}

type legacySessionTagsRequest struct {
	TeamID    int64
	SessionID int64
	Names     []string
}

type legacySessionCreateRequest struct {
	ActivityID int64
	At         time.Time
	Note       string
}
