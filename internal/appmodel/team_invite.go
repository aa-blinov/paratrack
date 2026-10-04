package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// TeamInviteResult is the workspace workflow's transport-neutral invitation
// result. The bearer token is available only to the workflow caller and must
// never be serialized as JSON.
type TeamInviteResult struct {
	Token      string `json:"-"`
	TeamID     int64
	Role       model.TeamRole
	CreatedAt  time.Time
	ExpiresAt  time.Time
	AcceptedAt time.Time
}

func (i TeamInviteResult) ExpiredAt(at time.Time) bool {
	return !i.ExpiresAt.IsZero() && !at.Before(i.ExpiresAt)
}

func (i TeamInviteResult) Used() bool { return !i.AcceptedAt.IsZero() }
