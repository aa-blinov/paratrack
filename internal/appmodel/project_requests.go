package appmodel

type AssignActivityProjectRequest struct {
	TeamID     int64
	ActivityID int64
	ProjectID  int64
	CallerID   int64
}

type ProjectMutationRequest struct {
	TeamID    int64
	ProjectID int64
	CallerID  int64
}

type ProjectUpdateRequest struct {
	TeamID    int64
	ProjectID int64
	CallerID  int64
	Update    ProjectUpdate
}

type ProjectRateRequest struct {
	TeamID    int64
	ProjectID int64
	CallerID  int64
	RateCents *int
	Billable  *bool
}
