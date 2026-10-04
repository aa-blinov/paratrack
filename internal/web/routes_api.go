package web

import "net/http"

func (s *Server) registerProtectedAPIRoutes(mux *http.ServeMux, apiAuth func(http.Handler) http.Handler) {
	// JSON API routes are registered directly on the top-level mux. The
	// apiAuth middleware makes authentication failures return JSON 401.
	// Form and HTMX submissions under /api paths remain on the pages mux
	// above, where unauthenticated requests redirect to the login page.
	api := func(method, path string, h http.HandlerFunc) {
		mux.Handle(method+" "+path, apiAuth(h))
	}

	api("GET", "/api/active", s.mine(s.handleAPIActive))
	api("GET", "/api/minibar", s.mine(s.handleMiniBar))
	api("GET", "/api/reports.csv", s.handleCSV)
	api("POST", "/api/start", s.mine(s.handleStart))
	api("POST", "/api/sessions/{id}/stop", s.mine(s.handleStop))
	api("POST", "/api/sessions/{id}/reopen", s.mine(s.handleReopen))
	api("POST", "/api/sessions/{id}/pause", s.mine(s.handlePause))
	api("POST", "/api/sessions/{id}/resume", s.mine(s.handleResume))
	api("POST", "/api/focus/{name}", s.mine(s.handleFocus))
	api("POST", "/api/active/pause-all", s.mine(s.handlePauseAll))
	api("POST", "/api/active/stop-all", s.mine(s.handleStopAll))
	api("POST", "/api/sessions/backfill", s.handleBackfill)
	api("PATCH", "/api/sessions/{id}", s.handleUpdateSession)
	api("DELETE", "/api/sessions/{id}", s.handleDeleteSession)
	api("GET", "/api/goals", s.module("goals", s.handleGoalsList))
	api("GET", "/api/goals/progress", s.module("goals", s.handleGoalsProgress))
	api("POST", "/api/goals", s.module("goals", s.manage(s.handleGoalsUpsert)))
	api("DELETE", "/api/goals", s.module("goals", s.manage(s.handleGoalsDelete)))
	api("GET", "/api/tags", s.handleTagsList)
	api("POST", "/api/tags", s.handleTagsCreate)
	api("DELETE", "/api/tags", s.manage(s.handleTagsDelete))
	api("POST", "/api/sessions/{id}/tags", s.handleSessionTagAdd)
	api("DELETE", "/api/sessions/{id}/tags", s.handleSessionTagRemove)

	// Workspace and profile management.
	api("POST", "/api/team/rename", s.manage(s.handleAPITeamRename))
	api("POST", "/api/team/currency", s.manage(s.handleAPITeamCurrency))
	api("POST", "/api/team/requisites", s.manage(s.handleAPITeamRequisites))
	api("POST", "/api/team/modules", s.manage(s.handleAPITeamModules))
	api("POST", "/api/team/billing", s.manage(s.handleAPITeamBilling))
	api("POST", "/api/team/logo", s.manage(s.handleAPITeamLogo))
	api("POST", "/api/me/preferences", s.handleAPIPreferences)
	api("POST", "/api/team/create", s.handleAPITeamCreate)
	api("POST", "/api/team/switch", s.handleAPITeamSwitch)
	api("POST", "/api/team/delete", s.handleAPITeamDelete)
	api("DELETE", "/api/team", s.handleAPITeamDelete)
	api("POST", "/api/team/invites", s.manage(s.handleAPIInviteCreate))
	api("POST", "/api/team/invites/{token}/revoke", s.manage(s.handleAPIInviteRevoke))
	api("DELETE", "/api/team/invites/{token}", s.manage(s.handleAPIInviteRevoke))
	api("POST", "/api/team/members/{id}/remove", s.handleAPIMemberRemove)
	api("POST", "/api/team/members/{id}/role", s.manage(s.handleAPIMemberRole))
	api("POST", "/api/team/transfer", s.manage(s.handleAPITeamTransfer))
	api("POST", "/api/invites/{token}/accept", s.handleAPIInviteAccept)
	api("POST", "/api/profile", s.handleAPIProfileUpdate)
	api("POST", "/api/profile/password", s.handleAPIProfilePassword)

	// Project API.
	api("GET", "/api/projects", s.handleAPIProjectsList)
	api("POST", "/api/projects", s.manage(s.handleAPIProjectCreate))
	api("PATCH", "/api/projects/{id}", s.manage(s.handleAPIProjectUpdate))
	api("DELETE", "/api/projects/{id}", s.manage(s.handleAPIProjectDelete))
	api("POST", "/api/activities/{id}/project", s.handleAPIAssignActivityProject)

	// Browser-extension endpoints use bearer API tokens.
	api("GET", "/api/me", s.handleAPIMe)
	api("GET", "/api/external-tasks", s.handleAPIExternalTasks)

	// Web Push API.
	api("GET", "/api/push/key", s.handlePushKey)
	api("POST", "/api/push/subscribe", s.handlePushSubscribe)
	api("POST", "/api/push/unsubscribe", s.handlePushUnsubscribe)

	// Versioned public API.
	api("GET", "/api/v1/sessions", s.handleAPIv1Sessions)
	api("POST", "/api/v1/sessions", s.handleAPIv1Sessions)
	api("PATCH", "/api/v1/sessions/{id}", s.handleAPIv1Session)
	api("DELETE", "/api/v1/sessions/{id}", s.handleAPIv1Session)
	api("GET", "/api/v1/projects", s.handleAPIv1Projects)
	api("GET", "/api/v1/reports/summary", s.handleAPIv1Report)

	// Every api registration above passes through apiAuth.
}
