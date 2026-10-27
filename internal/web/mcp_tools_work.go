package web

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// mcpTaskDescriptionLimit is the length the task workflow allows for a description.
const mcpTaskDescriptionLimit = 5000

// mcpWorkTools are the writes on projects, on a task's details (comments, checklist,
// dependencies, archive) and on time: a session written after the fact, edited or
// deleted, and the requests that change a timesheet cell. Each call runs the workflow
// the page calls with the token's caller, so the rule in that workflow decides what is
// allowed. The project writes are the manager's, as the project form is. task_get is the one
// read: the task page's own view of a task.
var mcpWorkTools = []mcpTool{
	{
		Name: "project_create",
		Description: "Create a project. Only a manager can. The slug is made from the name. The rate is in cents per hour, " +
			"the budget in cents, and the deadline is the day the project should be done.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"name":                stringProp("Project name"),
			"color":               stringProp("Colour as #rrggbb"),
			"client_id":           integerProp("Client card to link; 0 or left out for none"),
			"billable_rate_cents": integerProp("Hourly billable rate in cents"),
			"currency":            stringProp("Currency code, three capital letters, e.g. RUB"),
			"deadline":            dateProp("Day the project should be done"),
			"budget_cents":        integerProp("Budget ceiling in cents; 0 or left out for none"),
			"lead_user_id":        integerProp("Member who leads the project; 0 or left out for none"),
		}, "name"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Name              string `json:"name"`
				Color             string `json:"color"`
				ClientID          int64  `json:"client_id"`
				BillableRateCents *int   `json:"billable_rate_cents"`
				Currency          string `json:"currency"`
				Deadline          string `json:"deadline"`
				BudgetCents       *int   `json:"budget_cents"`
				LeadUserID        int64  `json:"lead_user_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			if in.Deadline != "" {
				if _, err := mcpDate("deadline", in.Deadline); err != nil {
					return nil, err
				}
			}
			if err := mcpNotBelowZero("client_id", in.ClientID); err != nil {
				return nil, err
			}
			if err := mcpNotBelowZero("lead_user_id", in.LeadUserID); err != nil {
				return nil, err
			}
			if in.BudgetCents != nil {
				if err := mcpNotBelowZero("budget_cents", int64(*in.BudgetCents)); err != nil {
					return nil, err
				}
			}
			p, err := s.services.Projects.Commands.Create(c.ctx, appmodel.ProjectCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, Name: in.Name, Color: in.Color, RateCents: in.BillableRateCents,
				Currency: in.Currency, BudgetCents: in.BudgetCents, Deadline: in.Deadline, LeadUserID: in.LeadUserID, ClientID: in.ClientID,
			})
			if err != nil {
				return nil, mcpProjectError(err)
			}
			return mcpProjectViewOf(p), nil
		},
	},
	{
		Name: "project_update",
		Description: "Change the fields of a project that are given; a field left out stays as it is. Only a manager can. " +
			"A client_id or lead_user_id of 0 clears it, an empty deadline clears the deadline, and a budget of 0 clears the ceiling.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"project_id":          integerProp("Project id"),
			"name":                stringProp("Project name"),
			"color":               stringProp("Colour as #rrggbb"),
			"client_id":           integerProp("Client card to link; 0 detaches it"),
			"billable":            booleanProp("Whether the project is invoiced"),
			"billable_rate_cents": integerProp("Hourly billable rate in cents"),
			"currency":            stringProp("Currency code, three capital letters; an empty string clears it"),
			"deadline":            dateProp("Day the project should be done; an empty string clears it"),
			"budget_cents":        integerProp("Budget ceiling in cents; 0 clears it"),
			"lead_user_id":        integerProp("Member who leads the project; 0 clears it"),
		}, "project_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ProjectID         int64   `json:"project_id"`
				Name              *string `json:"name"`
				Color             *string `json:"color"`
				ClientID          *int64  `json:"client_id"`
				Billable          *bool   `json:"billable"`
				BillableRateCents *int    `json:"billable_rate_cents"`
				Currency          *string `json:"currency"`
				Deadline          *string `json:"deadline"`
				BudgetCents       *int    `json:"budget_cents"`
				LeadUserID        *int64  `json:"lead_user_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			if in.ProjectID <= 0 {
				return nil, model.ErrNotFound
			}
			update := appmodel.ProjectUpdate{
				Billable: in.Billable, RateCents: in.BillableRateCents, Currency: in.Currency,
				ClientID: in.ClientID, Deadline: in.Deadline, BudgetCents: in.BudgetCents, LeadUserID: in.LeadUserID,
			}
			// An empty name or colour would be read as "leave it", so the agent is told which field it sent.
			if in.Name != nil {
				if strings.TrimSpace(*in.Name) == "" {
					return nil, appmodel.FieldError{Field: "name"}
				}
				update.Name = *in.Name
			}
			if in.Color != nil {
				if *in.Color == "" {
					return nil, appmodel.FieldError{Field: "color"}
				}
				update.Color = *in.Color
			}
			if in.Deadline != nil && *in.Deadline != "" {
				if _, err := mcpDate("deadline", *in.Deadline); err != nil {
					return nil, err
				}
			}
			if in.ClientID != nil {
				if err := mcpNotBelowZero("client_id", *in.ClientID); err != nil {
					return nil, err
				}
			}
			if in.LeadUserID != nil {
				if err := mcpNotBelowZero("lead_user_id", *in.LeadUserID); err != nil {
					return nil, err
				}
			}
			if in.BudgetCents != nil {
				if err := mcpNotBelowZero("budget_cents", int64(*in.BudgetCents)); err != nil {
					return nil, err
				}
			}
			p, err := s.services.Projects.Commands.Update(c.ctx, appmodel.ProjectUpdateRequest{
				TeamID: c.teamID, ProjectID: in.ProjectID, CallerID: c.userID, Update: update,
			})
			if err != nil {
				return nil, mcpProjectError(err)
			}
			return mcpProjectViewOf(p), nil
		},
	},
	{
		Name:        "project_archive",
		Description: "Archive a project, or restore it with archived set to false. Only a manager can.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"project_id": integerProp("Project id"),
			"archived":   booleanProp("true archives the project, false restores it"),
		}, "project_id", "archived"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ProjectID int64 `json:"project_id"`
				Archived  *bool `json:"archived"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			if in.ProjectID <= 0 {
				return nil, model.ErrNotFound
			}
			if in.Archived == nil {
				return nil, appmodel.FieldError{Field: "archived"}
			}
			p, err := s.services.Projects.Commands.Update(c.ctx, appmodel.ProjectUpdateRequest{
				TeamID: c.teamID, ProjectID: in.ProjectID, CallerID: c.userID, Update: appmodel.ProjectUpdate{Archived: in.Archived},
			})
			if err != nil {
				return nil, mcpProjectError(err)
			}
			return mcpProjectViewOf(p), nil
		},
	},
	{
		Name: "task_get",
		Description: "Read one task: its fields, its checklist, its feed, the tasks it waits for and the tasks that wait for it, " +
			"and how many people watch it. An archived task is read too.",
		Schema: objectSchema(map[string]any{"task_id": integerProp("Task id")}, "task_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				TaskID int64 `json:"task_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.TaskID <= 0 {
				return nil, model.ErrNotFound
			}
			rows, err := s.services.Tasks.List(c.ctx, appmodel.TaskQuery{TeamID: c.teamID, TaskID: in.TaskID, Now: c.now})
			if err != nil {
				return nil, err
			}
			if len(rows) == 0 {
				return nil, model.ErrNotFound
			}
			names, err := s.mcpMemberNames(c)
			if err != nil {
				return nil, err
			}
			steps, err := s.services.Tasks.Checklist(c.ctx, c.teamID, in.TaskID)
			if err != nil {
				return nil, err
			}
			comments, err := s.services.Tasks.Comments(c.ctx, c.teamID, in.TaskID)
			if err != nil {
				return nil, err
			}
			waits, blocks, err := s.services.Tasks.TaskDependencies(c.ctx, c.teamID, in.TaskID)
			if err != nil {
				return nil, err
			}
			watchers, err := s.services.Tasks.ListTaskWatchers(c.ctx, c.teamID, in.TaskID)
			if err != nil {
				return nil, err
			}
			view := mcpTaskDetailView{
				mcpTaskView:   mcpTaskViewOf(rows[0], names),
				Checklist:     make([]mcpChecklistItemView, 0, len(steps)),
				Comments:      make([]mcpTaskCommentView, 0, len(comments)),
				WaitsFor:      linkViews(waits),
				Blocks:        linkViews(blocks),
				WatchersCount: len(watchers),
			}
			for _, step := range steps {
				view.Checklist = append(view.Checklist, mcpChecklistItemView{ID: step.ID, TaskID: in.TaskID, Text: step.Body, Done: step.Done})
			}
			for _, cm := range comments {
				view.Comments = append(view.Comments, mcpTaskCommentView{ID: cm.ID, TaskID: in.TaskID, Author: cm.Author, Body: cm.Body, CreatedAt: cm.CreatedAt})
			}
			return view, nil
		},
	},
	{
		Name: "task_update",
		Description: "Change the fields of a task that are given; a field left out stays as it is. An empty due clears the due date, " +
			"an estimate of 0 clears the estimate, and an assignee of 0 clears the assignee. A status moves the task on the board " +
			"and notifies the watchers, except the person who made the change.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"task_id":          integerProp("Task id"),
			"title":            stringProp("Task title, up to 200 characters"),
			"description":      stringProp("What has to be done and how to tell it is done"),
			"assignee_id":      integerProp("Assignee user id; 0 clears it"),
			"due":              dateProp("Due date; an empty string clears it"),
			"estimate_seconds": integerProp("Estimate in seconds; kept in whole minutes; 0 clears it"),
			"status":           enumProp("Board column", mcpTaskStatuses...),
		}, "task_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				TaskID          int64   `json:"task_id"`
				Title           *string `json:"title"`
				Description     *string `json:"description"`
				AssigneeID      *int64  `json:"assignee_id"`
				Due             *string `json:"due"`
				EstimateSeconds *int    `json:"estimate_seconds"`
				Status          string  `json:"status"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.TaskID <= 0 {
				return nil, model.ErrNotFound
			}
			if in.Status != "" && !slices.Contains(mcpTaskStatuses, in.Status) {
				return nil, appmodel.FieldError{Field: "status"}
			}
			if in.Due != nil && *in.Due != "" {
				if _, err := mcpDate("due", *in.Due); err != nil {
					return nil, err
				}
			}
			if in.Description != nil && utf8.RuneCountInString(*in.Description) > mcpTaskDescriptionLimit {
				return nil, appmodel.FieldError{Field: "description"}
			}
			if in.AssigneeID != nil {
				if err := mcpNotBelowZero("assignee_id", *in.AssigneeID); err != nil {
					return nil, err
				}
			}
			if in.EstimateSeconds != nil {
				if err := mcpNotBelowZero("estimate_seconds", int64(*in.EstimateSeconds)); err != nil {
					return nil, err
				}
			}
			request := appmodel.TaskUpdateRequest{
				TeamID: c.teamID, CallerID: c.userID, TaskID: in.TaskID,
				Title: in.Title, AssigneeID: in.AssigneeID, Due: in.Due, Description: in.Description,
			}
			if in.EstimateSeconds != nil {
				minutes := *in.EstimateSeconds / 60
				request.EstimateMinutes = &minutes
			}
			if _, err := s.services.Tasks.Update(c.ctx, request); err != nil {
				return nil, mcpTaskError(err, "title")
			}
			if in.Status != "" {
				if err := s.services.Tasks.SetStatus(c.ctx, appmodel.TaskStatusRequest{
					TeamID: c.teamID, CallerID: c.userID, TaskID: in.TaskID, Status: in.Status,
				}); err != nil {
					return nil, mcpTaskError(err, "status")
				}
				s.notifyTaskWatchers(c.req, in.TaskID, i18n.T(c.lang, "watch.statusTitle"), in.Status)
			}
			return struct {
				TaskID int64 `json:"task_id"`
			}{in.TaskID}, nil
		},
	},
	{
		Name:        "task_comment_add",
		Description: "Add a line to a task's feed. The watchers of the task are notified, except the person who wrote it.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"task_id": integerProp("Task id"),
			"body":    stringProp("The line to add"),
		}, "task_id", "body"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				TaskID int64  `json:"task_id"`
				Body   string `json:"body"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.TaskID <= 0 {
				return nil, model.ErrNotFound
			}
			comment, err := s.services.Tasks.AddComment(c.ctx, appmodel.TaskCommentRequest{
				TeamID: c.teamID, TaskID: in.TaskID, CallerID: c.userID, Body: in.Body,
			})
			if err != nil {
				return nil, mcpTaskError(err, "body")
			}
			s.notifyTaskWatchers(c.req, in.TaskID, i18n.T(c.lang, "watch.commentTitle"), comment.Body)
			return mcpTaskCommentView{ID: comment.ID, TaskID: in.TaskID, Author: comment.Author, Body: comment.Body, CreatedAt: comment.CreatedAt}, nil
		},
	},
	{
		Name:        "task_checklist_add",
		Description: "Add a step to the end of a task's checklist.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"task_id": integerProp("Task id"),
			"text":    stringProp("The step, up to 200 characters"),
		}, "task_id", "text"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				TaskID int64  `json:"task_id"`
				Text   string `json:"text"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.TaskID <= 0 {
				return nil, model.ErrNotFound
			}
			item, err := s.services.Tasks.AddChecklistItem(c.ctx, appmodel.TaskChecklistAddRequest{
				TeamID: c.teamID, TaskID: in.TaskID, CallerID: c.userID, Body: in.Text,
			})
			if err != nil {
				return nil, mcpTaskError(err, "text")
			}
			return mcpChecklistItemView{ID: item.ID, TaskID: in.TaskID, Text: item.Body, Done: item.Done}, nil
		},
	},
	{
		Name:        "task_checklist_set_done",
		Description: "Mark a step of a task's checklist done, or not done again.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"task_id": integerProp("Task id"),
			"item_id": integerProp("Checklist step id"),
			"done":    booleanProp("true marks the step done"),
		}, "task_id", "item_id", "done"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				TaskID int64 `json:"task_id"`
				ItemID int64 `json:"item_id"`
				Done   *bool `json:"done"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.TaskID <= 0 || in.ItemID <= 0 {
				return nil, model.ErrNotFound
			}
			if in.Done == nil {
				return nil, appmodel.FieldError{Field: "done"}
			}
			if err := s.services.Tasks.SetChecklistItem(c.ctx, appmodel.TaskChecklistSetRequest{
				TeamID: c.teamID, TaskID: in.TaskID, CallerID: c.userID, ItemID: in.ItemID, Done: *in.Done,
			}); err != nil {
				return nil, mcpTaskError(err, "item_id")
			}
			return struct {
				TaskID int64 `json:"task_id"`
				ItemID int64 `json:"item_id"`
				Done   bool  `json:"done"`
			}{in.TaskID, in.ItemID, *in.Done}, nil
		},
	},
	{
		Name:        "task_checklist_delete",
		Description: "Delete a step from a task's checklist. The deletion is permanent and cannot be undone.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"task_id": integerProp("Task id"),
			"item_id": integerProp("Checklist step id"),
		}, "task_id", "item_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				TaskID int64 `json:"task_id"`
				ItemID int64 `json:"item_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.TaskID <= 0 || in.ItemID <= 0 {
				return nil, model.ErrNotFound
			}
			if err := s.services.Tasks.DeleteChecklistItem(c.ctx, appmodel.TaskChecklistDeleteRequest{
				TeamID: c.teamID, TaskID: in.TaskID, CallerID: c.userID, ItemID: in.ItemID,
			}); err != nil {
				return nil, mcpTaskError(err, "item_id")
			}
			return struct {
				TaskID  int64 `json:"task_id"`
				ItemID  int64 `json:"item_id"`
				Deleted bool  `json:"deleted"`
			}{in.TaskID, in.ItemID, true}, nil
		},
	},
	{
		Name: "task_dependency_add",
		Description: "Make a task wait for another task of the same project. A task cannot wait for itself, nor for a task " +
			"that waits for it, directly or through other tasks.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"task_id":            integerProp("The task that waits"),
			"depends_on_task_id": integerProp("The task it waits for"),
		}, "task_id", "depends_on_task_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				TaskID          int64 `json:"task_id"`
				DependsOnTaskID int64 `json:"depends_on_task_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.TaskID <= 0 || in.DependsOnTaskID <= 0 {
				return nil, model.ErrNotFound
			}
			if err := s.services.Tasks.AddTaskDependency(c.ctx, appmodel.TaskDependencyRequest{
				TeamID: c.teamID, CallerID: c.userID, TaskID: in.TaskID, DependsOnID: in.DependsOnTaskID,
			}); err != nil {
				return nil, mcpTaskError(err, "depends_on_task_id")
			}
			return mcpDependencyView{TaskID: in.TaskID, DependsOnTaskID: in.DependsOnTaskID}, nil
		},
	},
	{
		Name:        "task_dependency_remove",
		Description: "Remove a wait: the task no longer waits for the other one. The wait can be added again.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"task_id":            integerProp("The task that waits"),
			"depends_on_task_id": integerProp("The task it waits for"),
		}, "task_id", "depends_on_task_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				TaskID          int64 `json:"task_id"`
				DependsOnTaskID int64 `json:"depends_on_task_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.TaskID <= 0 || in.DependsOnTaskID <= 0 {
				return nil, model.ErrNotFound
			}
			if err := s.services.Tasks.DeleteTaskDependency(c.ctx, appmodel.TaskDependencyRequest{
				TeamID: c.teamID, CallerID: c.userID, TaskID: in.TaskID, DependsOnID: in.DependsOnTaskID,
			}); err != nil {
				return nil, mcpTaskError(err, "depends_on_task_id")
			}
			return mcpDependencyView{TaskID: in.TaskID, DependsOnTaskID: in.DependsOnTaskID}, nil
		},
	},
	{
		Name: "task_archive",
		Description: "Archive a task, taking it off the board, or restore it with archived set to false. Its time stays. " +
			"A manager or the task's creator can do it.",
		Write:  true,
		Schema: objectSchema(map[string]any{"task_id": integerProp("Task id"), "archived": booleanProp("true archives, false restores")}, "task_id", "archived"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				TaskID   int64 `json:"task_id"`
				Archived *bool `json:"archived"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.TaskID <= 0 {
				return nil, model.ErrNotFound
			}
			if in.Archived == nil {
				return nil, appmodel.FieldError{Field: "archived"}
			}
			if *in.Archived {
				err = s.services.Tasks.Archive(c.ctx, appmodel.TaskArchiveRequest{TeamID: c.teamID, CallerID: c.userID, TaskID: in.TaskID})
			} else {
				err = s.services.Tasks.Restore(c.ctx, appmodel.TaskRestoreRequest{TeamID: c.teamID, CallerID: c.userID, TaskID: in.TaskID})
			}
			if err != nil {
				return nil, mcpTaskError(err, "task_id")
			}
			return struct {
				TaskID   int64 `json:"task_id"`
				Archived bool  `json:"archived"`
			}{in.TaskID, *in.Archived}, nil
		},
	},
	{
		Name: "time_session_add",
		Description: "Add a session that is already over, under an activity by name; the activity is created when the name is new. " +
			"A session longer than ten hours needs confirm_long, as the form asks once. An approved week refuses the session, " +
			"and the reply says which weeks are locked.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"activity":     stringProp("Activity name"),
			"project_id":   integerProp("Project the session counts under, when it differs from the activity's"),
			"start":        localDateTimeProp("Start"),
			"end":          localDateTimeProp("End, after the start"),
			"note":         stringProp("Note on the session"),
			"confirm_long": booleanProp("Set true to keep a session longer than ten hours"),
		}, "activity", "start", "end"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Activity    string `json:"activity"`
				ProjectID   int64  `json:"project_id"`
				Start       string `json:"start"`
				End         string `json:"end"`
				Note        string `json:"note"`
				ConfirmLong bool   `json:"confirm_long"`
			}](args)
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(in.Activity)
			if name == "" {
				return nil, appmodel.FieldError{Field: "activity"}
			}
			if err := mcpNotBelowZero("project_id", in.ProjectID); err != nil {
				return nil, err
			}
			loc := c.now.Location()
			start, err := mcpMoment("start", in.Start, loc)
			if err != nil {
				return nil, err
			}
			end, err := mcpMoment("end", in.End, loc)
			if err != nil {
				return nil, err
			}
			if !end.After(start) {
				return nil, appmodel.FieldError{Field: "end"}
			}
			if end.Sub(start) > longSessionLimit && !in.ConfirmLong {
				return nil, mcpRefusal{key: "stats.longSessionConfirm"}
			}
			activity, session, err := s.services.TrackingOps.AddClosedActivity(operationContext(c.req), appmodel.TimerAddByNameRequest{
				TeamID: c.teamID, CallerID: c.userID, ActivityName: name, ProjectID: in.ProjectID,
				Start: start, End: end, Note: strings.TrimSpace(in.Note),
			})
			if errors.Is(err, appmodel.ErrTaskArchived) || errors.Is(err, appmodel.ErrInvalidSessionStart) {
				return nil, appmodel.FieldError{Field: "activity"}
			}
			if err != nil {
				return nil, mcpSessionPeriodError(err)
			}
			return s.mcpSessionOf(c, session, activity)
		},
	},
	{
		Name: "time_session_update",
		Description: "Change the start, end, note or project of a session; a field left out stays as it is. A member changes only " +
			"their own sessions, and a session in an approved week stays locked. A project_id of 0 returns the session to its " +
			"activity's project.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"session_id": integerProp("Session id"),
			"start":      localDateTimeProp("Start"),
			"end":        localDateTimeProp("End, after the start"),
			"note":       stringProp("Note on the session; an empty string clears it"),
			"project_id": integerProp("Project of the session; 0 returns it to the activity's project"),
		}, "session_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				SessionID int64   `json:"session_id"`
				Start     *string `json:"start"`
				End       *string `json:"end"`
				Note      *string `json:"note"`
				ProjectID *int64  `json:"project_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.SessionID <= 0 {
				return nil, model.ErrNotFound
			}
			loc := c.now.Location()
			update := appmodel.SessionUpdate{UpdatedAt: c.now, ProjectID: in.ProjectID}
			if in.Start != nil {
				start, err := mcpMoment("start", *in.Start, loc)
				if err != nil {
					return nil, err
				}
				update.StartAt = &start
			}
			if in.End != nil {
				end, err := mcpMoment("end", *in.End, loc)
				if err != nil {
					return nil, err
				}
				update.EndAt = &end
			}
			if in.ProjectID != nil {
				if err := mcpNotBelowZero("project_id", *in.ProjectID); err != nil {
					return nil, err
				}
			}
			// The edit writes the note on every update, as the session form does, so a note
			// left out is read back from the session and written as it is.
			if in.Note != nil {
				update.Note = *in.Note
			} else {
				current, err := s.services.Tracking.Queries.Session(c.ctx, c.teamID, in.SessionID)
				if err != nil {
					return nil, err
				}
				if current.Note != nil {
					update.Note = *current.Note
				}
			}
			if err := s.services.Tracking.Commands.UpdateFields(c.ctx, appmodel.SessionUpdateRequest{
				TeamID: c.teamID, CallerID: c.userID, SessionID: in.SessionID, IP: clientIP(c.req),
				RecomputeDuration: in.Start != nil || in.End != nil, Update: update,
			}); err != nil {
				return nil, mcpSessionPeriodError(err)
			}
			session, err := s.services.Tracking.Queries.Session(c.ctx, c.teamID, in.SessionID)
			if err != nil {
				return nil, err
			}
			activity, err := s.services.Tracking.Queries.Activity(c.ctx, c.teamID, session.ActivityID)
			if err != nil {
				return nil, err
			}
			return s.mcpSessionOf(c, session, activity)
		},
	},
	{
		Name: "time_session_delete",
		Description: "Delete a time session. A member deletes only their own sessions, and a session in an approved week stays locked. " +
			"The deletion is permanent and cannot be undone.",
		Write:       true,
		Destructive: true,
		Schema:      objectSchema(map[string]any{"session_id": integerProp("Session id")}, "session_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				SessionID int64 `json:"session_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if in.SessionID <= 0 {
				return nil, model.ErrNotFound
			}
			if err := s.services.TrackingOps.Delete(operationContext(c.req), appmodel.SessionDeleteRequest{
				TeamID: c.teamID, CallerID: c.userID, SessionID: in.SessionID,
			}); err != nil {
				return nil, err
			}
			return struct {
				SessionID int64 `json:"session_id"`
				Deleted   bool  `json:"deleted"`
			}{in.SessionID, true}, nil
		},
	},
	{
		Name: "time_edit_request_create",
		Description: "Ask for a change to the time of one activity on one day, as a timesheet cell asks. The proposed minutes become " +
			"that day's total for the activity once a manager approves. The activity is given by id, or by name when the id is not " +
			"known. Anyone may ask, and the request always concerns the asker's own time.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"activity_id": integerProp("Activity id; give it, or the activity name"),
			"activity":    stringProp("Activity name, used when activity_id is left out"),
			"day":         dateProp("Day of the timesheet cell"),
			"minutes":     integerProp("Proposed total minutes for that activity on that day, 0 to 1440"),
			"reason":      stringProp("Why the time has to change"),
		}, "day", "minutes", "reason"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ActivityID int64  `json:"activity_id"`
				Activity   string `json:"activity"`
				Day        string `json:"day"`
				Minutes    int    `json:"minutes"`
				Reason     string `json:"reason"`
			}](args)
			if err != nil {
				return nil, err
			}
			if err := mcpNotBelowZero("activity_id", in.ActivityID); err != nil {
				return nil, err
			}
			activityID := in.ActivityID
			if activityID == 0 {
				name := strings.TrimSpace(in.Activity)
				if name == "" {
					return nil, appmodel.FieldError{Field: "activity_id"}
				}
				// A lookup, not a find-or-create: an unknown name is a not-found answer.
				activity, err := s.services.Tracking.Queries.FindActivity(c.ctx, appmodel.ActivityNameQuery{TeamID: c.teamID, Name: name})
				if err != nil {
					return nil, err
				}
				activityID = activity.ID
			}
			day, err := mcpDate("day", in.Day)
			if err != nil {
				return nil, err
			}
			if in.Minutes < 0 || in.Minutes > 24*60 {
				return nil, appmodel.FieldError{Field: "minutes"}
			}
			if strings.TrimSpace(in.Reason) == "" {
				return nil, appmodel.FieldError{Field: "reason"}
			}
			created, err := s.services.Tracking.Commands.RequestTimeEdit(c.ctx, appmodel.TimeEditCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, ActivityID: activityID, Day: day, Minutes: in.Minutes, Reason: in.Reason,
			})
			if errors.Is(err, appmodel.ErrInvalidTask) {
				return nil, appmodel.FieldError{Field: "reason"}
			}
			if err != nil {
				return nil, err
			}
			return mcpTimeEditView{
				ID: created.ID, Activity: created.ActivityName, Day: created.Day.Format(time.DateOnly),
				Minutes: created.Minutes, Reason: created.Reason, Status: created.Status,
			}, nil
		},
	},
	{
		Name: "time_edit_request_decide",
		Description: "Approve or refuse a pending time edit request. An approved request sets the minutes of its activity on its day " +
			"as the requester's time. Only a manager can decide.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"request_id": integerProp("Time edit request id"),
			"approve":    booleanProp("true approves the request, false refuses it"),
			"note":       stringProp("Note for the requester"),
		}, "request_id", "approve"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				RequestID int64  `json:"request_id"`
				Approve   *bool  `json:"approve"`
				Note      string `json:"note"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			if in.RequestID <= 0 {
				return nil, model.ErrNotFound
			}
			if in.Approve == nil {
				return nil, appmodel.FieldError{Field: "approve"}
			}
			if err := s.services.Tracking.Commands.DecideTimeEdit(c.ctx, appmodel.TimeEditDecisionRequest{
				TeamID: c.teamID, CallerID: c.userID, RequestID: in.RequestID, Approve: *in.Approve, Note: in.Note,
			}); err != nil {
				// The workflow answers a request that is no longer pending, or a note too long, with the invalid-input error.
				if errors.Is(err, appmodel.ErrInvalidTask) {
					return nil, appmodel.FieldError{Field: "request_id"}
				}
				return nil, err
			}
			status := "rejected"
			if *in.Approve {
				status = "approved"
			}
			return struct {
				RequestID int64  `json:"request_id"`
				Status    string `json:"status"`
			}{in.RequestID, status}, nil
		},
	},
}

type mcpTaskCommentView struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type mcpChecklistItemView struct {
	ID     int64  `json:"id"`
	TaskID int64  `json:"task_id"`
	Text   string `json:"text"`
	Done   bool   `json:"done"`
}

type mcpTimeEditView struct {
	ID       int64  `json:"id"`
	Activity string `json:"activity"`
	Day      string `json:"day"`
	Minutes  int    `json:"minutes"`
	Reason   string `json:"reason"`
	Status   string `json:"status"`
}

// mcpTaskDetailView is a task as task_get shows it: its fields, then its checklist, feed,
// the tasks it waits for and blocks, and how many people watch it.
type mcpTaskDetailView struct {
	mcpTaskView
	Checklist     []mcpChecklistItemView `json:"checklist"`
	Comments      []mcpTaskCommentView   `json:"comments"`
	WaitsFor      []taskLinkView         `json:"waits_for"`
	Blocks        []taskLinkView         `json:"blocks"`
	WatchersCount int                    `json:"watchers_count"`
}

// mcpRefusal is a refusal the page words with a translation key and that names no field,
// such as a session over ten hours that the page asks the person to confirm.
type mcpRefusal struct {
	key string
}

func (e mcpRefusal) Error() string { return e.key }

// mcpWorkErrorText reads a refusal of a session, a time edit or a week in the words the
// page uses for it. An input problem the page names by its field is a FieldError, which
// the dispatcher names; the tools map those where they call the workflow.
func mcpWorkErrorText(c mcpCall, err error) (string, bool) {
	var refusal mcpRefusal
	var approvalErr *model.SessionApprovalLockError
	var autoLockErr *model.SessionAutoLockError
	switch {
	case errors.As(err, &refusal):
		return i18n.T(c.lang, refusal.key), true
	case errors.As(err, &approvalErr):
		return weekLockedMessage(c.lang, c.now.Location(), approvalErr), true
	case errors.As(err, &autoLockErr):
		return autoLockMessage(c.lang, autoLockErr), true
	case errors.Is(err, model.ErrWeekApproved):
		return i18n.T(c.lang, "tse.weekApproved"), true
	case errors.Is(err, model.ErrProjectRebindForbidden):
		return i18n.T(c.lang, "act.rebindForbidden"), true
	}
	return "", false
}

// mcpProjectError names the field behind a refused project write. A name another project
// already has is the only conflict the workflow reports.
func mcpProjectError(err error) error {
	switch {
	case errors.Is(err, model.ErrAlreadyExists), errors.Is(err, appmodel.ErrInvalidProjectName):
		return appmodel.FieldError{Field: "name"}
	case errors.Is(err, appmodel.ErrInvalidProjectColor):
		return appmodel.FieldError{Field: "color"}
	case errors.Is(err, appmodel.ErrInvalidProjectRate):
		return appmodel.FieldError{Field: "billable_rate_cents"}
	case errors.Is(err, appmodel.ErrInvalidProjectCurrency):
		return appmodel.FieldError{Field: "currency"}
	}
	return err
}

// mcpTaskError names the field behind a refused task write, and reads a task that is not
// there as a missing entity. An archived task refuses the write, named by its id as
// task_set_status names it.
func mcpTaskError(err error, field string) error {
	switch {
	case errors.Is(err, appmodel.ErrTaskNotFound):
		return model.ErrNotFound
	case errors.Is(err, appmodel.ErrTaskArchived):
		return appmodel.FieldError{Field: "task_id"}
	case errors.Is(err, appmodel.ErrInvalidTask):
		return appmodel.FieldError{Field: field}
	}
	return err
}

// mcpSessionPeriodError names the end of a session period the workflow refuses: an end at
// or before the start, or a session longer than the store keeps.
func mcpSessionPeriodError(err error) error {
	switch {
	case errors.Is(err, appmodel.ErrInvalidSessionPeriod), errors.Is(err, appmodel.ErrSessionEndInFuture), errors.Is(err, appmodel.ErrInvalidSessionLength), errors.Is(err, model.ErrSessionDurationOverflow):
		return appmodel.FieldError{Field: "end"}
	}
	return err
}

// mcpNotBelowZero names the field of an id or an amount that cannot be negative. The
// workflows read a negative id as no value, so the agent is told instead.
func mcpNotBelowZero(field string, value int64) error {
	if value < 0 {
		return appmodel.FieldError{Field: field}
	}
	return nil
}

// mcpMoment reads a moment as the calendar does: RFC 3339, or a wall clock in the
// workspace zone. An empty value is refused, because the session needs both edges.
func mcpMoment(field, value string, loc *time.Location) (time.Time, error) {
	moment, err := calendarMoment(field, value, loc)
	if err != nil {
		return time.Time{}, err
	}
	if moment.IsZero() {
		return time.Time{}, appmodel.FieldError{Field: field}
	}
	return moment, nil
}

// mcpSessionOf is a session as the time list shows it, with the task its activity is held by.
func (s *Server) mcpSessionOf(c mcpCall, session model.Session, activity model.Activity) (mcpSessionView, error) {
	taskID, err := s.services.Tasks.TaskOfActivity(c.ctx, c.teamID, activity.ID)
	if err != nil {
		return mcpSessionView{}, err
	}
	view := mcpSessionView{
		ID: session.ID, Activity: activity.Name, ProjectID: activity.ProjectID, TaskID: taskID,
		Start: session.StartAt, End: session.EndAt, Seconds: session.DurationSeconds(c.now),
	}
	if session.Note != nil {
		view.Note = *session.Note
	}
	return view, nil
}

func mcpProjectViewOf(p model.Project) mcpProjectView {
	return mcpProjectView{
		ID: p.ID, Slug: p.Slug, Name: p.Name, Color: p.Color, Archived: p.Archived,
		Billable: p.Billable, ClientID: p.ClientID, BillableRateCents: p.BillableRateCents,
	}
}

// mcpDependencyView is the link a dependency call changed: the waiting task and the one it waits for.
type mcpDependencyView struct {
	TaskID          int64 `json:"task_id"`
	DependsOnTaskID int64 `json:"depends_on_task_id"`
}
