package appmodel

import "github.com/aa-blinov/paratrack/internal/model"

type TeamMemberManagementSnapshot struct {
	Members     []model.TeamMember
	PaySettings []model.MemberPayrollSettings
}

// TeamInvitePageSnapshot contains the invitation and its workspace for the
// public invitation page. Empty values represent an unknown or deleted invite.
type TeamInvitePageSnapshot struct {
	Invite TeamInviteResult
	Team   model.Team
}

// IntegrationManagementSnapshot contains connected providers with task counts
