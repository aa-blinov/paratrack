package web

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// mcpEntityTools holds the tools for the entities that predate CRM: clients,
// projects, tasks, time sessions and the report summary.
var mcpEntityTools = []mcpTool{
	{
		Name:        "clients_list",
		Description: "List the workspace's clients with their currency and state. Email, details and notes are filled in for owners and admins only.",
		Schema:      objectSchema(map[string]any{"archived": booleanProp("Include archived clients")}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Archived bool `json:"archived"`
			}](args)
			if err != nil {
				return nil, err
			}
			list, err := s.services.Clients.List(c.ctx, appmodel.ClientCatalogQuery{TeamID: c.teamID, IncludeArchived: in.Archived})
			if err != nil {
				return nil, err
			}
			out := make([]mcpClientView, 0, len(list))
			for _, client := range list {
				out = append(out, mcpClientViewOf(client, c.manage))
			}
			return struct {
				Clients []mcpClientView `json:"clients"`
			}{out}, nil
		},
	},
	{
		Name:        "client_create",
		Description: "Create a client. Only owners and admins can add one, and its name must be unique in the workspace.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"name":     stringProp("Client name, unique in the workspace"),
			"email":    stringProp("Billing email"),
			"details":  stringProp("Requisites"),
			"currency": stringProp("Three-letter currency code; empty means the workspace currency"),
			"notes":    stringProp("Internal notes"),
		}, "name"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Name     string `json:"name"`
				Email    string `json:"email"`
				Details  string `json:"details"`
				Currency string `json:"currency"`
				Notes    string `json:"notes"`
			}](args)
			if err != nil {
				return nil, err
			}
			if err := checkMCPClient(in.Name, in.Email, in.Currency); err != nil {
				return nil, err
			}
			client, err := s.services.Clients.Create(c.ctx, appmodel.ClientCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, Name: in.Name, Email: in.Email,
				Details: in.Details, Currency: in.Currency, Notes: in.Notes,
			})
			if err != nil {
				return nil, mcpClientWriteError(err)
			}
			return mcpClientViewOf(client, c.manage), nil
		},
	},
	{
		Name: "client_update",
		Description: "Change the fields of a client that are given; a field left out stays as it is. Only owners and admins can change a client. " +
			"Set archived to hide a client from the pickers, since clients cannot be deleted.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"client_id": integerProp("Client id"),
			"name":      stringProp("Client name, unique in the workspace"),
			"email":     stringProp("Billing email"),
			"details":   stringProp("Requisites"),
			"notes":     stringProp("Internal notes"),
			"currency":  stringProp("Three-letter currency code; empty clears it to the workspace currency"),
			"archived":  booleanProp("Archived clients are hidden from the pickers"),
		}, "client_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ClientID int64   `json:"client_id"`
				Name     *string `json:"name"`
				Email    *string `json:"email"`
				Details  *string `json:"details"`
				Notes    *string `json:"notes"`
				Currency *string `json:"currency"`
				Archived *bool   `json:"archived"`
			}](args)
			if err != nil {
				return nil, err
			}
			current, err := s.services.Clients.GetInTeam(c.ctx, appmodel.ClientScopeQuery{TeamID: c.teamID, ClientID: in.ClientID})
			if err != nil {
				return nil, err
			}
			update := clientUpdatePreserving(current)
			if in.Name != nil {
				update.Name = *in.Name
			}
			if in.Email != nil {
				update.Email = *in.Email
			}
			if in.Details != nil {
				update.Details = *in.Details
			}
			if in.Notes != nil {
				update.Notes = *in.Notes
			}
			if in.Currency != nil {
				update.Currency = in.Currency
			}
			if in.Archived != nil {
				update.Archived = in.Archived
			}
			// clientUpdatePreserving always sets the currency, so the dereference is safe.
			if err := checkMCPClient(update.Name, update.Email, *update.Currency); err != nil {
				return nil, err
			}
			client, err := s.services.Clients.Update(c.ctx, appmodel.ClientUpdateRequest{
				TeamID: c.teamID, ClientID: in.ClientID, CallerID: c.userID, Update: update,
			})
			if err != nil {
				return nil, mcpClientWriteError(err)
			}
			return mcpClientViewOf(client, c.manage), nil
		},
	},
	{
		Name:        "projects_list",
		Description: "List the workspace's projects with their colour, client and billability. The billable rate is filled in for owners and admins only.",
		Schema:      objectSchema(map[string]any{"archived": booleanProp("Include archived projects")}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Archived bool `json:"archived"`
			}](args)
			if err != nil {
				return nil, err
			}
			list, err := s.services.Projects.Queries.List(c.ctx, appmodel.ProjectCatalogQuery{TeamID: c.teamID, IncludeArchived: in.Archived})
			if err != nil {
				return nil, err
			}
			out := make([]mcpProjectView, 0, len(list))
			for _, p := range list {
				view := mcpProjectView{
					ID: p.ID, Slug: p.Slug, Name: p.Name, Color: p.Color, Archived: p.Archived,
					Billable: p.Billable, ClientID: p.ClientID,
				}
				if c.manage {
					view.BillableRateCents = p.BillableRateCents
				}
				out = append(out, view)
			}
			return struct {
				Projects []mcpProjectView `json:"projects"`
			}{out}, nil
		},
	},
	{
		Name:        "tasks_list",
		Description: "List the task board, or the archive when archived is true, for the whole workspace or one project. Durations are in seconds.",
		Schema: objectSchema(map[string]any{
			"project_id": integerProp("Only the tasks of this project"),
			"archived":   booleanProp("List the archive instead of the open board"),
		}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ProjectID int64 `json:"project_id"`
				Archived  bool  `json:"archived"`
			}](args)
			if err != nil {
				return nil, err
			}
			rows, err := s.services.Tasks.List(c.ctx, appmodel.TaskQuery{TeamID: c.teamID, ProjectID: in.ProjectID, Archived: in.Archived, Now: c.now})
			if err != nil {
				return nil, err
			}
			names, err := s.mcpMemberNames(c)
			if err != nil {
				return nil, err
			}
			out := make([]mcpTaskView, 0, len(rows))
			for _, row := range rows {
				out = append(out, mcpTaskViewOf(row, names))
			}
			return struct {
				Tasks []mcpTaskView `json:"tasks"`
			}{out}, nil
		},
	},
	{
		Name:        "task_create",
		Description: "Create a task in a project. The status defaults to todo, and the estimate is given in seconds and kept in whole minutes.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"project_id":       integerProp("Project id"),
			"title":            stringProp("Task title, up to 200 characters"),
			"status":           enumProp("Board column", mcpTaskStatuses...),
			"assignee_id":      integerProp("Assignee user id"),
			"due":              dateProp("Due date"),
			"estimate_seconds": integerProp("Estimate in seconds"),
			"description":      stringProp("What has to be done and how to tell it is done"),
		}, "project_id", "title"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ProjectID       int64  `json:"project_id"`
				Title           string `json:"title"`
				Status          string `json:"status"`
				AssigneeID      int64  `json:"assignee_id"`
				Due             string `json:"due"`
				EstimateSeconds int    `json:"estimate_seconds"`
				Description     string `json:"description"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.Status != "" && !slices.Contains(mcpTaskStatuses, in.Status) {
				return nil, appmodel.FieldError{Field: "status"}
			}
			if in.Due != "" {
				if _, err := mcpDate("due", in.Due); err != nil {
					return nil, err
				}
			}
			if in.EstimateSeconds < 0 {
				return nil, appmodel.FieldError{Field: "estimate_seconds"}
			}
			task, err := s.services.Tasks.Create(c.ctx, appmodel.TaskCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, ProjectID: in.ProjectID, Title: in.Title, Status: in.Status,
				AssigneeID: in.AssigneeID, Due: in.Due, EstimateMinutes: in.EstimateSeconds / 60, Description: in.Description,
			})
			if err != nil {
				// The service refuses only the title here: the status and the estimate were checked above.
				if errors.Is(err, appmodel.ErrInvalidTask) {
					return nil, appmodel.FieldError{Field: "title"}
				}
				return nil, err
			}
			return struct {
				ID int64 `json:"id"`
			}{task.ID}, nil
		},
	},
	{
		Name:        "task_set_status",
		Description: "Move a task to another board column. The watchers of the task are notified, except the person who made the change.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"task_id": integerProp("Task id"),
			"status":  enumProp("Board column", mcpTaskStatuses...),
		}, "task_id", "status"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				TaskID int64  `json:"task_id"`
				Status string `json:"status"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !slices.Contains(mcpTaskStatuses, in.Status) {
				return nil, appmodel.FieldError{Field: "status"}
			}
			err = s.services.Tasks.SetStatus(c.ctx, appmodel.TaskStatusRequest{TeamID: c.teamID, CallerID: c.userID, TaskID: in.TaskID, Status: in.Status})
			if errors.Is(err, appmodel.ErrTaskNotFound) {
				return nil, model.ErrNotFound
			}
			if errors.Is(err, appmodel.ErrTaskArchived) {
				return nil, appmodel.FieldError{Field: "task_id"}
			}
			if err != nil {
				return nil, err
			}
			// The change has happened, so a failed notification is logged, not returned (TASK-12).
			ids, err := s.services.Tasks.TaskWatcherIDs(c.ctx, c.teamID, in.TaskID)
			if err != nil {
				s.logInternalError(err)
			} else if others := slices.DeleteFunc(ids, func(id int64) bool { return id == c.userID }); len(others) > 0 {
				if err := s.services.Push.NotifyUsers(c.ctx, appmodel.PushNotifyRequest{
					TeamID: c.teamID, UserIDs: others, Title: i18n.T(c.lang, "watch.statusTitle"), Body: in.Status,
					URL: "/tasks/" + strconv.FormatInt(in.TaskID, 10), Topic: appmodel.NotificationTopicTaskWatch,
				}); err != nil {
					s.logInternalError(err)
				}
			}
			return struct {
				TaskID int64  `json:"task_id"`
				Status string `json:"status"`
			}{in.TaskID, in.Status}, nil
		},
	},
	{
		Name:        "time_session_start",
		Description: "Start a timer on a named activity. A task that holds the activity moves to doing. The call fails while a timer already runs on the activity.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"activity":   stringProp("Activity name"),
			"project_id": integerProp("Project the session counts under, when it differs from the activity's"),
			"note":       stringProp("Note on the session"),
		}, "activity"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Activity  string `json:"activity"`
				ProjectID int64  `json:"project_id"`
				Note      string `json:"note"`
			}](args)
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(in.Activity)
			if name == "" {
				return nil, appmodel.FieldError{Field: "activity"}
			}
			activity, session, err := s.services.TrackingOps.StartActivity(c.ctx, appmodel.TimerStartByNameRequest{
				TeamID: c.teamID, CallerID: c.userID, ActivityName: name, ProjectID: in.ProjectID,
				At: c.now, Note: strings.TrimSpace(in.Note),
			})
			if errors.Is(err, model.ErrActiveSessionExists) || errors.Is(err, appmodel.ErrTaskArchived) {
				// The dispatcher has no text for these conflicts, so the activity is named as the refused input.
				return nil, appmodel.FieldError{Field: "activity"}
			}
			if err != nil {
				return nil, err
			}
			// Work that starts on a task moves it to doing, as the timer form does (TIME-04).
			if taskID, err := s.services.Tasks.TaskOfActivity(c.ctx, c.teamID, activity.ID); err == nil && taskID > 0 {
				if err := s.services.Tasks.StartWork(c.ctx, appmodel.TaskStartRequest{TeamID: c.teamID, TaskID: taskID, CallerID: c.userID}); err != nil {
					s.logInternalError(fmt.Errorf("mark task %d started: %w", taskID, err))
				}
			}
			return struct {
				SessionID int64  `json:"session_id"`
				Activity  string `json:"activity"`
			}{session.ID, activity.Name}, nil
		},
	},
	{
		Name:        "time_session_stop",
		Description: "Stop a running timer by its session id. Returns the activity and the seconds the session ran.",
		Write:       true,
		Schema:      objectSchema(map[string]any{"session_id": integerProp("Session id of the running timer")}, "session_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				SessionID int64 `json:"session_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			stopped, err := s.services.TrackingOps.Stop(c.ctx, appmodel.TimerStopRequest{TeamID: c.teamID, SessionID: in.SessionID, At: c.now})
			if errors.Is(err, model.ErrSessionNotActive) {
				// Same stopgap as time_session_start: the session is already stopped.
				return nil, appmodel.FieldError{Field: "session_id"}
			}
			if err != nil {
				return nil, err
			}
			return struct {
				SessionID int64  `json:"session_id"`
				Activity  string `json:"activity"`
				Seconds   int    `json:"seconds"`
			}{in.SessionID, stopped.ActivityName, stopped.DurationSeconds}, nil
		},
	},
	{
		Name:        "time_sessions_list",
		Description: "List time sessions, newest first, for the days from to to (both inclusive). Without dates it covers the last 30 days. Members see only their own sessions. Pass next_cursor as cursor to get the next page.",
		Schema: objectSchema(map[string]any{
			"from":   dateProp("First day"),
			"to":     dateProp("Last day"),
			"limit":  integerProp("Sessions per page, 1 to 500; 100 when left out"),
			"cursor": stringProp("next_cursor of the previous page"),
		}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				From   string `json:"from"`
				To     string `json:"to"`
				Limit  int    `json:"limit"`
				Cursor string `json:"cursor"`
			}](args)
			if err != nil {
				return nil, err
			}
			// The dates are read as midnight UTC, the same window the JSON API uses.
			from, to := c.now.AddDate(0, 0, -30), c.now.Add(24*time.Hour)
			if in.From != "" {
				if from, err = mcpDate("from", in.From); err != nil {
					return nil, err
				}
			}
			if in.To != "" {
				last, err := mcpDate("to", in.To)
				if err != nil {
					return nil, err
				}
				to = last.AddDate(0, 0, 1)
			}
			var after *appmodel.SessionCursor
			if in.Cursor != "" {
				cursor, ok := decodeSessionCursor(in.Cursor)
				if !ok {
					return nil, appmodel.FieldError{Field: "cursor"}
				}
				after = cursor
			}
			page, err := s.services.Tracking.Queries.SessionHistoryPage(c.ctx, appmodel.SessionHistoryPageQuery{
				TeamID: c.teamID, From: from, To: to, After: after, Limit: in.Limit,
			})
			if err != nil {
				return nil, err
			}
			linked, err := s.services.Tasks.LinkedActivityTasks(c.ctx, c.teamID)
			if err != nil {
				return nil, err
			}
			out := make([]mcpSessionView, 0, len(page.Items))
			for _, item := range page.Items {
				view := mcpSessionView{
					ID: item.Session.ID, Activity: item.Activity.Name, ProjectID: item.Activity.ProjectID,
					TaskID: linked[item.Activity.ID].TaskID, Start: item.Session.StartAt, End: item.Session.EndAt,
					Seconds: item.Session.DurationSeconds(c.now),
				}
				if item.Session.Note != nil {
					view.Note = *item.Session.Note
				}
				out = append(out, view)
			}
			resp := mcpSessionPage{Sessions: out}
			if page.NextCursor != nil {
				resp.NextCursor = encodeSessionCursor(*page.NextCursor)
			}
			return resp, nil
		},
	},
	{
		Name:        "report_summary",
		Description: "Total tracked seconds for the days from to to (both inclusive), and the seconds per activity.",
		Schema: objectSchema(map[string]any{
			"from": dateProp("First day"),
			"to":   dateProp("Last day"),
		}, "from", "to"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				From string `json:"from"`
				To   string `json:"to"`
			}](args)
			if err != nil {
				return nil, err
			}
			from, err := mcpDate("from", in.From)
			if err != nil {
				return nil, err
			}
			last, err := mcpDate("to", in.To)
			if err != nil {
				return nil, err
			}
			if last.Before(from) {
				return nil, appmodel.FieldError{Field: "to"}
			}
			result, err := s.services.ReportBuilder.Build(c.ctx, appmodel.ReportBuildQuery{
				TeamID: c.teamID, From: from, To: last.AddDate(0, 0, 1), Now: c.now,
				GroupBy: appmodel.ReportGroupActivity, Labels: appmodel.ReportLabels{
					Uncategorized: i18n.T(c.lang, "dash.uncategorized"),
				},
			})
			if err != nil {
				return nil, err
			}
			byActivity := make(map[string]int, len(result.Rows))
			labels := appmodel.ActivityLabels(result.Rows)
			for i, row := range result.Rows {
				byActivity[labels[i]] = row.Seconds
			}
			return struct {
				From         string         `json:"from"`
				To           string         `json:"to"`
				TotalSeconds int            `json:"total_seconds"`
				ByActivity   map[string]int `json:"by_activity"`
			}{in.From, in.To, result.TotalSeconds, byActivity}, nil
		},
	},
}

// mcpTaskStatuses are the board columns. The task service refuses any other status.
var mcpTaskStatuses = []string{model.TaskStatusTodo, model.TaskStatusDoing, model.TaskStatusDone}

// checkMCPClient refuses a client field the client form would refuse, before the
// write, so the agent is told which field to fix.
func checkMCPClient(name, email, currency string) error {
	if strings.TrimSpace(name) == "" {
		return appmodel.FieldError{Field: "name"}
	}
	if trimmed := strings.TrimSpace(email); trimmed != "" {
		if _, err := mail.ParseAddress(trimmed); err != nil {
			return appmodel.FieldError{Field: "email"}
		}
	}
	if currency != "" && !validCurrency(currency) {
		return appmodel.FieldError{Field: "currency"}
	}
	return nil
}

// mcpClientWriteError names the field behind a refused client write. A name that
// another client already has is the only conflict the directory reports.
func mcpClientWriteError(err error) error {
	if errors.Is(err, model.ErrAlreadyExists) {
		return appmodel.FieldError{Field: "name"}
	}
	return err
}

// mcpDate reads a YYYY-MM-DD argument and names the field when it is not one.
func mcpDate(field, value string) (time.Time, error) {
	day, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, appmodel.FieldError{Field: field}
	}
	return day, nil
}

// The views below are the JSON shapes of the entities that predate CRM. Dates are
// YYYY-MM-DD, times are RFC 3339 and durations are whole seconds.

type mcpClientView struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Details   string    `json:"details"`
	Currency  string    `json:"currency"`
	Notes     string    `json:"notes"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
}

func mcpClientViewOf(client model.Client, manage bool) mcpClientView {
	view := mcpClientView{ID: client.ID, Name: client.Name, Currency: client.Currency, Archived: client.Archived, CreatedAt: client.CreatedAt}
	if manage {
		view.Email, view.Details, view.Notes = client.Email, client.Details, client.Notes
	}
	return view
}

type mcpProjectView struct {
	ID                int64  `json:"id"`
	Slug              string `json:"slug"`
	Name              string `json:"name"`
	Color             string `json:"color"`
	Archived          bool   `json:"archived"`
	Billable          bool   `json:"billable"`
	ClientID          int64  `json:"client_id"`
	BillableRateCents *int   `json:"billable_rate_cents"`
}

type mcpActivity struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type mcpTaskView struct {
	ID              int64         `json:"id"`
	ProjectID       int64         `json:"project_id"`
	Title           string        `json:"title"`
	Description     string        `json:"description"`
	Status          string        `json:"status"`
	AssigneeID      int64         `json:"assignee_id"`
	AssigneeName    string        `json:"assignee_name"`
	DueDate         string        `json:"due_date"`
	EstimateSeconds int           `json:"estimate_seconds"`
	TrackedSeconds  int           `json:"tracked_seconds"`
	Archived        bool          `json:"archived"`
	ExternalURL     string        `json:"external_url"`
	Activities      []mcpActivity `json:"activities"`
}

// mcpMemberNames maps each member of the workspace to the name the tools show for them.
func (s *Server) mcpMemberNames(c mcpCall) (map[int64]string, error) {
	members, err := s.services.Teams.Directory.Members(c.ctx, c.teamID)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(members))
	for _, m := range members {
		names[m.UserID] = cmp.Or(m.Name, m.Email)
	}
	return names, nil
}

// mcpTaskViewOf is one task as the task tools show it, with its assignee's name.
func mcpTaskViewOf(row appmodel.TaskRow, names map[int64]string) mcpTaskView {
	t := row.Task
	view := mcpTaskView{
		ID: t.ID, ProjectID: t.ProjectID, Title: t.Title, Description: t.Description, Status: t.Status,
		AssigneeID: t.AssigneeID, AssigneeName: names[t.AssigneeID], DueDate: t.Due,
		EstimateSeconds: t.EstimateMinutes * 60, TrackedSeconds: row.TrackedMin * 60,
		Archived: t.Archived, ExternalURL: t.ExternalURL, Activities: []mcpActivity{},
	}
	for _, a := range row.Activities {
		view.Activities = append(view.Activities, mcpActivity{ID: a.ID, Name: a.Name, Color: a.Color})
	}
	return view
}

type mcpSessionView struct {
	ID        int64      `json:"id"`
	Activity  string     `json:"activity"`
	ProjectID int64      `json:"project_id"`
	TaskID    int64      `json:"task_id"`
	Start     time.Time  `json:"start"`
	End       *time.Time `json:"end"`
	Seconds   int        `json:"seconds"`
	Note      string     `json:"note"`
}

type mcpSessionPage struct {
	Sessions   []mcpSessionView `json:"sessions"`
	NextCursor string           `json:"next_cursor,omitempty"`
}
