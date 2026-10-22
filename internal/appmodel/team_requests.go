package appmodel

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/model"
)

// TeamMembershipQuery resolves a user's membership within one workspace.
type TeamMembershipQuery struct {
	TeamID int64
	UserID int64
}

type TeamMemberRoleRequest struct {
	TeamID       int64
	TargetUserID int64
	CallerID     int64
	Role         model.TeamRole
}

type TeamOwnershipTransferRequest struct {
	TeamID     int64
	CallerID   int64
	NewOwnerID int64
}

type TeamMemberRemovalRequest struct {
	TeamID       int64
	TargetUserID int64
	CallerID     int64
	LeftAt       time.Time
}

// TeamMemberReturnRequest brings a former member back into the workspace with the
// pay they left with (ADM-04).
type TeamMemberReturnRequest struct {
	TeamID       int64
	TargetUserID int64
	CallerID     int64
}
