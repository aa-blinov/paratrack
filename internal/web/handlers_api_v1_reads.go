package web

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

// The read side of the public API for the things a move needs besides time:
// clients, tags and the task board. A read-only token may call all three.

type apiV1Client struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Details   string    `json:"details"`
	Currency  string    `json:"currency"`
	Notes     string    `json:"notes"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
}

type apiV1ClientsResponse struct {
	Clients []apiV1Client `json:"clients"`
}

func (apiV1ClientsResponse) isJSONResponse() {}

// handleAPIv1Clients lists the workspace's clients. ?archived=1 includes the
// archived ones.
func (s *Server) handleAPIv1Clients(w http.ResponseWriter, r *http.Request) {
	list, err := s.services.Clients.List(r.Context(), appmodel.ClientCatalogQuery{
		TeamID: teamID(r), IncludeArchived: r.URL.Query().Get("archived") == "1",
	})
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	// Contacts, requisites and internal notes are for the people who bill (D-2).
	// A member gets the client's name, its currency and its state.
	manage := canManage(r)
	out := make([]apiV1Client, 0, len(list))
	for _, c := range list {
		row := apiV1Client{ID: c.ID, Name: c.Name, Currency: c.Currency, Archived: c.Archived, CreatedAt: c.CreatedAt}
		if manage {
			row.Email, row.Details, row.Notes = c.Email, c.Details, c.Notes
		}
		out = append(out, row)
	}
	s.writeJSON(w, apiV1ClientsResponse{Clients: out})
}

type apiV1Tag struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
	SessionCount int       `json:"session_count"`
}

type apiV1TagsResponse struct {
	Tags []apiV1Tag `json:"tags"`
}

func (apiV1TagsResponse) isJSONResponse() {}

// handleAPIv1Tags lists the workspace's tags with how many sessions carry each.
func (s *Server) handleAPIv1Tags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.services.Tagging.Queries.ListWithCounts(r.Context(), appmodel.TagListQuery{TeamID: teamID(r)})
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	out := make([]apiV1Tag, 0, len(tags))
	for _, tag := range tags {
		out = append(out, apiV1Tag{ID: tag.ID, Name: tag.Name, CreatedAt: tag.CreatedAt, SessionCount: tag.SessionCount})
	}
	s.writeJSON(w, apiV1TagsResponse{Tags: out})
}

type apiV1Activity struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type apiV1Task struct {
	ID              int64           `json:"id"`
	ProjectID       int64           `json:"project_id"`
	Title           string          `json:"title"`
	Status          string          `json:"status"`
	AssigneeID      int64           `json:"assignee_id"`
	AssigneeName    string          `json:"assignee_name"`
	DueDate         string          `json:"due_date"`
	EstimateMinutes int             `json:"estimate_minutes"`
	TrackedMinutes  int             `json:"tracked_minutes"`
	Archived        bool            `json:"archived"`
	ExternalURL     string          `json:"external_url"`
	Activities      []apiV1Activity `json:"activities"`
}

type apiV1TasksResponse struct {
	Tasks []apiV1Task `json:"tasks"`
}

func (apiV1TasksResponse) isJSONResponse() {}

// handleAPIv1Tasks lists the task board. ?project=<id> narrows it to one project,
// and ?archived=1 lists the archive instead.
func (s *Server) handleAPIv1Tasks(w http.ResponseWriter, r *http.Request) {
	projectID, _ := strconv.ParseInt(r.URL.Query().Get("project"), 10, 64)
	items, err := s.taskItems(r, appmodel.TaskQuery{ProjectID: projectID, Archived: r.URL.Query().Get("archived") == "1"})
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	out := make([]apiV1Task, 0, len(items))
	for _, item := range items {
		task := apiV1Task{
			ID: item.ID, ProjectID: item.ProjectID, Title: item.Title, Status: item.Status,
			AssigneeID: item.AssigneeID, AssigneeName: item.AssigneeName, DueDate: item.Due,
			EstimateMinutes: item.EstimateMin, TrackedMinutes: item.TrackedMin, Archived: item.Archived,
			ExternalURL: item.ExternalURL, Activities: []apiV1Activity{},
		}
		for _, a := range item.Activities {
			task.Activities = append(task.Activities, apiV1Activity{ID: a.ID, Name: a.Name, Color: a.Color})
		}
		out = append(out, task)
	}
	s.writeJSON(w, apiV1TasksResponse{Tasks: out})
}
