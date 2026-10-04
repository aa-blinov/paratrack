package model

import (
	"time"
)

// Tag is a free-form label attachable to sessions.
type Tag struct {
	ID        int64
	Name      string
	TeamID    int64
	CreatedAt time.Time
}

// SessionTag links a session to a tag (many-to-many).
type SessionTag struct {
	SessionID int64
	TagID     int64
}
