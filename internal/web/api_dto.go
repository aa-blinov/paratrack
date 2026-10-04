package web

import (
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

type dashboardPageResponse struct {
	Data dashboardData `json:"data"`
}

func (dashboardPageResponse) isJSONResponse() {}

type apiErrorResponse struct {
	Error string `json:"error"`
}

func (apiErrorResponse) isJSONResponse() {}

type webManifestResponse struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	ShortName       string                `json:"short_name"`
	Description     string                `json:"description"`
	Language        string                `json:"lang"`
	StartURL        string                `json:"start_url"`
	Scope           string                `json:"scope"`
	Display         string                `json:"display"`
	BackgroundColor string                `json:"background_color"`
	ThemeColor      string                `json:"theme_color"`
	Categories      []string              `json:"categories"`
	Icons           []webManifestIcon     `json:"icons"`
	Shortcuts       []webManifestShortcut `json:"shortcuts"`
}

func (webManifestResponse) isJSONResponse() {}

type webManifestIcon struct {
	Source  string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

type webManifestShortcut struct {
	Name      string            `json:"name"`
	ShortName string            `json:"short_name"`
	URL       string            `json:"url"`
	Icons     []webManifestIcon `json:"icons"`
}

// These response types keep the public JSON contract in the HTTP adapter.
// Persistence and workflow models can then evolve without changing API
// fields implicitly through encoding tags.
type projectResponse struct {
	ID                int64     `json:"id"`
	TeamID            int64     `json:"team_id"`
	Slug              string    `json:"slug"`
	Name              string    `json:"name"`
	Color             string    `json:"color"`
	Archived          bool      `json:"archived"`
	EstimateMinutes   *int      `json:"estimate_minutes,omitempty"`
	BillableRateCents *int      `json:"billable_rate_cents,omitempty"`
	Billable          bool      `json:"billable"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (projectResponse) isJSONResponse() {}

func projectsFor(r *http.Request, list []model.Project) []projectResponse {
	responses := make([]projectResponse, 0, len(list))
	for _, project := range list {
		responses = append(responses, projectFor(r, project))
	}
	return responses
}

func projectFor(r *http.Request, project model.Project) projectResponse {
	response := projectResponse{
		ID: project.ID, TeamID: project.TeamID, Slug: project.Slug,
		Name: project.Name, Color: project.Color, Archived: project.Archived,
		EstimateMinutes: project.EstimateMinutes, Billable: project.Billable,
		CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
	}
	if canManage(r) {
		response.BillableRateCents = project.BillableRateCents
	}
	return response
}

type goalResponse struct {
	ID            int64     `json:"id"`
	ActivityID    int64     `json:"activity_id"`
	TeamID        int64     `json:"team_id"`
	Period        string    `json:"period"`
	TargetMinutes int       `json:"target_minutes"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (goalResponse) isJSONResponse() {}

func goalFor(goal model.Goal) goalResponse {
	return goalResponse{
		ID: goal.ID, ActivityID: goal.ActivityID, TeamID: goal.TeamID,
		Period: goal.Period, TargetMinutes: goal.TargetMinutes,
		CreatedAt: goal.CreatedAt, UpdatedAt: goal.UpdatedAt,
	}
}

func goalsFor(goals []model.Goal) []goalResponse {
	responses := make([]goalResponse, 0, len(goals))
	for _, goal := range goals {
		responses = append(responses, goalFor(goal))
	}
	return responses
}

type goalProgressResponse struct {
	Goal            goalResponse `json:"goal"`
	ActivityName    string       `json:"activity_name"`
	AchievedMinutes int          `json:"achieved_minutes"`
	PercentComplete int          `json:"percent_complete"`
	PeriodStart     time.Time    `json:"period_start"`
	PeriodEnd       time.Time    `json:"period_end"`
}

func (goalProgressResponse) isJSONResponse() {}

func goalProgressFor(progress []appmodel.GoalProgress) []goalProgressResponse {
	responses := make([]goalProgressResponse, 0, len(progress))
	for _, item := range progress {
		responses = append(responses, goalProgressResponse{
			Goal: goalFor(item.Goal), ActivityName: item.ActivityName,
			AchievedMinutes: item.AchievedMinutes, PercentComplete: item.PercentComplete,
			PeriodStart: item.PeriodStart, PeriodEnd: item.PeriodEnd,
		})
	}
	return responses
}

type tagResponse struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	TeamID       int64     `json:"team_id"`
	CreatedAt    time.Time `json:"created_at"`
	SessionCount int       `json:"session_count"`
}

func (tagResponse) isJSONResponse() {}

type apiV1SessionResponse struct {
	ID       int64  `json:"id"`
	Activity string `json:"activity"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Seconds  int    `json:"seconds"`
	Note     string `json:"note"`
}

type apiV1SessionsResponse struct {
	Sessions   []apiV1SessionResponse `json:"sessions"`
	NextCursor *string                `json:"next_cursor,omitempty"`
}

func (apiV1SessionsResponse) isJSONResponse() {}

type apiV1SessionCreateResponse struct {
	SessionID int64  `json:"session_id"`
	Activity  string `json:"activity"`
}

func (apiV1SessionCreateResponse) isJSONResponse() {}

type apiV1SessionUpdateResponse struct {
	Updated int64 `json:"updated"`
}

func (apiV1SessionUpdateResponse) isJSONResponse() {}

type apiV1SessionDeleteResponse struct {
	Deleted int64 `json:"deleted"`
}

func (apiV1SessionDeleteResponse) isJSONResponse() {}

type apiV1ProjectsResponse struct {
	Projects []projectResponse `json:"projects"`
}

func (apiV1ProjectsResponse) isJSONResponse() {}

type apiV1ReportResponse struct {
	From         string         `json:"from"`
	To           string         `json:"to"`
	TotalSeconds int            `json:"total_seconds"`
	ByActivity   map[string]int `json:"by_activity"`
}

func (apiV1ReportResponse) isJSONResponse() {}

type projectListResponse struct {
	Projects []projectResponse `json:"projects"`
}

func (projectListResponse) isJSONResponse() {}

type goalsListResponse struct {
	Goals []goalResponse `json:"goals"`
}

func (goalsListResponse) isJSONResponse() {}

type goalProgressListResponse struct {
	Progress []goalProgressResponse `json:"progress"`
}

func (goalProgressListResponse) isJSONResponse() {}

type tagListResponse struct {
	Tags []tagResponse `json:"tags"`
}

func (tagListResponse) isJSONResponse() {}

type pushPublicKeyResponse struct {
	PublicKey string `json:"publicKey"`
}

func (pushPublicKeyResponse) isJSONResponse() {}

type operationOKResponse struct {
	OK bool `json:"ok"`
}

func (operationOKResponse) isJSONResponse() {}

type apiMeUserResponse struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type apiMeTeamResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type apiMeResponse struct {
	User apiMeUserResponse `json:"user"`
	Team apiMeTeamResponse `json:"team"`
}

func (apiMeResponse) isJSONResponse() {}

type externalTaskResponse struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Status   string `json:"status"`
	Provider string `json:"provider"`
}

type externalTasksResponse struct {
	Tasks []externalTaskResponse `json:"tasks"`
}

func (externalTasksResponse) isJSONResponse() {}

func tagFor(tag model.Tag) tagResponse {
	return tagResponse{ID: tag.ID, Name: tag.Name, TeamID: tag.TeamID, CreatedAt: tag.CreatedAt}
}
