package web

import (
	"encoding/json"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// mcpCRMTools is the CRM and calendar tool set. Each tool calls the same workflow
// as the HTTP route for it, so the rules are the same for a person and an agent.
var mcpCRMTools = []mcpTool{
	{
		Name: "funnel_report",
		Description: "The sales funnel for a period: deals per pipeline stage now, deals won and lost in the period, the win rate " +
			"won/(won+lost) (null without closed deals), open deals whose next step has passed, leads by status now, and leads " +
			"created in the period by source with how many became deals, with the first-response figures of those leads. " +
			"Days are YYYY-MM-DD, both included, in the workspace zone. Amounts per currency come only to a manager; a member gets counts.",
		Schema: objectSchema(map[string]any{
			"from": dateProp("First day of the period"),
			"to":   dateProp("Last day of the period"),
		}, "from", "to"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				From string `json:"from"`
				To   string `json:"to"`
			}](args)
			if err != nil {
				return nil, err
			}
			start, end, err := leadPeriod(in.From, in.To, c.now.Location())
			if err != nil {
				return nil, err
			}
			report, err := s.services.CRM.FunnelReport(c.ctx, appmodel.CRMFunnelQuery{
				TeamID: c.teamID, CallerID: c.userID, From: start, To: end, Today: c.today,
			})
			if err != nil {
				return nil, err
			}
			return crmFunnelViewOf(report, c.lang, in.From, in.To, c.today), nil
		},
	},
	{
		Name:        "contacts_list",
		Description: "List the contacts of one client: name, role, email and phone.",
		Schema:      objectSchema(map[string]any{"client_id": integerProp("Client id")}, "client_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ClientID int64 `json:"client_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			contacts, err := s.services.CRM.Contacts(c.ctx, appmodel.CRMContactListQuery{TeamID: c.teamID, CallerID: c.userID, ClientID: in.ClientID})
			if err != nil {
				return nil, err
			}
			return crmContactsResponse{Contacts: crmContactViews(contacts)}, nil
		},
	},
	{
		Name:        "contact_create",
		Description: "Add a contact to a client. Look the client up with clients_list and the contacts with contacts_list first, so the same person is not added twice.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"client_id": integerProp("Client id"),
			"name":      stringProp("Full name"),
			"role":      stringProp("Role at the client, for example buyer or engineer"),
			"email":     stringProp("Email"),
			"phone":     stringProp("Phone"),
		}, "client_id", "name"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ClientID int64  `json:"client_id"`
				Name     string `json:"name"`
				Role     string `json:"role"`
				Email    string `json:"email"`
				Phone    string `json:"phone"`
			}](args)
			if err != nil {
				return nil, err
			}
			contact, err := s.services.CRM.CreateContact(c.ctx, appmodel.CRMContactCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, ClientID: in.ClientID, Name: in.Name,
				Role: in.Role, Email: in.Email, Phone: in.Phone,
			})
			if err != nil {
				return nil, err
			}
			return crmContactViewOf(contact), nil
		},
	},
	{
		Name: "deals_list",
		Description: "List the deals of the workspace with the pipeline counts per stage and the number of overdue open deals. " +
			"A deal is overdue when its next step date is before today and its stage is open. Filter by stage key, owner or overdue.",
		Schema: objectSchema(map[string]any{
			"stage":    stringProp("Only deals in this stage: a stage key from deal_stages_list"),
			"owner_id": integerProp("Only deals of this owner"),
			"overdue":  booleanProp("Only open deals whose next step has passed"),
		}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Stage   string `json:"stage"`
				OwnerID int64  `json:"owner_id"`
				Overdue bool   `json:"overdue"`
			}](args)
			if err != nil {
				return nil, err
			}
			base := appmodel.CRMDealListQuery{TeamID: c.teamID, CallerID: c.userID, Today: c.today}
			query := base
			query.Stage, query.OwnerID, query.Overdue = in.Stage, in.OwnerID, in.Overdue
			deals, err := s.services.CRM.Deals(c.ctx, query)
			if err != nil {
				return nil, err
			}
			pipeline, err := s.services.CRM.Pipeline(c.ctx, base)
			if err != nil {
				return nil, err
			}
			return crmDealsResponse{Today: c.today, Deals: crmDealViews(deals, c.today), Pipeline: crmPipelineViewOf(pipeline, c.lang)}, nil
		},
	},
	{
		Name: "deal_create",
		Description: "Create a deal. The stage defaults to the workspace's first open stage and the owner to the token's user. " +
			"A deal cannot start in a lost stage. " +
			"Amounts are in cents of the deal currency, and probability is a percentage from 0 to 100.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"title":        stringProp("Deal title"),
			"stage":        stringProp("Stage key from deal_stages_list; empty means the first open stage"),
			"client_id":    integerProp("Client id"),
			"contact_id":   integerProp("Contact id of the client"),
			"owner_id":     integerProp("Owner user id; another owner needs a manager"),
			"amount_cents": integerProp("Amount in cents"),
			"currency":     stringProp("Three-letter currency code; empty means the workspace currency"),
			"probability":  integerProp("Chance to win, 0 to 100"),
			"close_date":   dateProp("Expected close date"),
			"next_step_at": dateProp("Date of the next step"),
		}, "title"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[crmDealBody](args)
			if err != nil {
				return nil, err
			}
			deal, err := s.services.CRM.CreateDeal(c.ctx, appmodel.CRMDealCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, Title: in.Title, Stage: in.Stage, ClientID: in.ClientID,
				ContactID: in.ContactID, OwnerID: in.OwnerID, AmountCents: in.AmountCents, Currency: in.Currency,
				Probability: in.Probability, CloseDate: in.CloseDate, NextStepAt: in.NextStepAt,
			})
			if err != nil {
				return nil, err
			}
			return crmDealViewOf(deal, c.today), nil
		},
	},
	{
		Name:        "deal_update",
		Description: "Change the fields of a deal that are given. A field that is left out stays as it is. The stage changes with deal_move_stage, not here.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"deal_id":      integerProp("Deal id"),
			"title":        stringProp("Deal title"),
			"owner_id":     integerProp("Owner user id; changing it needs a manager"),
			"client_id":    integerProp("Client id; 0 clears it"),
			"contact_id":   integerProp("Contact id; 0 clears it"),
			"amount_cents": integerProp("Amount in cents"),
			"probability":  integerProp("Chance to win, 0 to 100"),
			"close_date":   dateProp("Expected close date; empty clears it"),
			"next_step_at": dateProp("Date of the next step; empty clears it"),
		}, "deal_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				DealID int64 `json:"deal_id"`
				crmDealUpdateBody
			}](args)
			if err != nil {
				return nil, err
			}
			deal, err := s.services.CRM.UpdateDeal(c.ctx, appmodel.CRMDealUpdateRequest{
				TeamID: c.teamID, CallerID: c.userID, DealID: in.DealID, Title: in.Title, OwnerID: in.OwnerID,
				ClientID: in.ClientID, ContactID: in.ContactID, AmountCents: in.AmountCents,
				Probability: in.Probability, CloseDate: in.CloseDate, NextStepAt: in.NextStepAt,
			})
			if err != nil {
				return nil, err
			}
			return crmDealViewOf(deal, c.today), nil
		},
	},
	{
		Name: "deal_move_stage",
		Description: "Move a deal to another stage. A stage of kind lost needs a lost_reason. Leaving a lost stage clears the reason. " +
			"Only the deal owner or a manager can move a deal.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"deal_id":     integerProp("Deal id"),
			"stage":       stringProp("New stage: a stage key from deal_stages_list"),
			"lost_reason": stringProp("Why the deal is lost; required when the stage is of kind lost"),
		}, "deal_id", "stage"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				DealID     int64  `json:"deal_id"`
				Stage      string `json:"stage"`
				LostReason string `json:"lost_reason"`
			}](args)
			if err != nil {
				return nil, err
			}
			deal, err := s.services.CRM.MoveDealStage(c.ctx, appmodel.CRMDealStageRequest{
				TeamID: c.teamID, CallerID: c.userID, DealID: in.DealID, Stage: in.Stage, LostReason: in.LostReason,
			})
			if err != nil {
				return nil, err
			}
			return crmDealViewOf(deal, c.today), nil
		},
	},
	{
		Name:        "deal_stages_list",
		Description: "List the deal pipeline of the workspace in order: each stage's key, name, position and kind. A deal's stage is the key. The kind is open, won or lost.",
		Schema:      objectSchema(map[string]any{}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			if _, err := mcpArgs[struct{}](args); err != nil {
				return nil, err
			}
			stages, err := s.services.CRM.Stages(c.ctx, appmodel.CRMStageListQuery{TeamID: c.teamID, CallerID: c.userID})
			if err != nil {
				return nil, err
			}
			return crmStagesResponse{Stages: crmStageConfigViews(stages, c.lang)}, nil
		},
	},
	{
		Name: "deal_create_project",
		Description: "Make the project of a won deal: named as the deal, for its client, with the deal's amount as the budget. " +
			"The deal must be in a won stage, have a client and have no project yet. The owner or a manager asks, and only a manager " +
			"creates the project. Set the billable rate afterwards with project_update.",
		Write:  true,
		Schema: objectSchema(map[string]any{"deal_id": integerProp("Deal id")}, "deal_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				DealID int64 `json:"deal_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			deal, err := s.services.CRM.CreateDealProject(c.ctx, appmodel.CRMDealProjectRequest{
				TeamID: c.teamID, CallerID: c.userID, DealID: in.DealID,
			})
			if err != nil {
				return nil, err
			}
			return crmDealViewOf(deal, c.today), nil
		},
	},
	{
		Name: "deal_stage_create",
		Description: "Add a stage to the end of the pipeline. The key is derived from the name. The kind decides the rules: " +
			"an open stage can be overdue, a won or lost stage closes the deal, and a lost stage needs a reason. Managers only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"name": stringProp("Stage name as people read it"),
			"kind": enumProp("Kind of stage; defaults to open", "open", "won", "lost"),
		}, "name"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
			}](args)
			if err != nil {
				return nil, err
			}
			stage, err := s.services.CRM.CreateStage(c.ctx, appmodel.CRMStageCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, Manager: c.manage, Name: in.Name, Kind: in.Kind,
			})
			if err != nil {
				return nil, err
			}
			return crmStageConfigViewOf(stage, c.lang), nil
		},
	},
	{
		Name: "deal_stage_update",
		Description: "Rename a stage, change its kind or move it to a 1-based position; the stages in between shift. " +
			"The key does not change, and the pipeline keeps an open, a won and a lost stage. Managers only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"key":      stringProp("Stage key from deal_stages_list"),
			"name":     stringProp("New name"),
			"kind":     enumProp("New kind of stage", "open", "won", "lost"),
			"position": integerProp("New 1-based position"),
		}, "key"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Key      string  `json:"key"`
				Name     *string `json:"name"`
				Kind     *string `json:"kind"`
				Position *int    `json:"position"`
			}](args)
			if err != nil {
				return nil, err
			}
			stage, err := s.services.CRM.UpdateStage(c.ctx, appmodel.CRMStageUpdateRequest{
				TeamID: c.teamID, CallerID: c.userID, Manager: c.manage, Key: in.Key,
				Name: in.Name, Kind: in.Kind, Position: in.Position,
			})
			if err != nil {
				return nil, err
			}
			return crmStageConfigViewOf(stage, c.lang), nil
		},
	},
	{
		Name: "deal_stage_delete",
		Description: "Delete a stage that has no deals. A stage with deals is refused, so move its deals first with deal_move_stage. " +
			"Managers only, and the deletion cannot be undone.",
		Write:       true,
		Destructive: true,
		Schema:      objectSchema(map[string]any{"key": stringProp("Stage key from deal_stages_list")}, "key"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Key string `json:"key"`
			}](args)
			if err != nil {
				return nil, err
			}
			if err := s.services.CRM.DeleteStage(c.ctx, appmodel.CRMStageDeleteRequest{
				TeamID: c.teamID, CallerID: c.userID, Manager: c.manage, Key: in.Key,
			}); err != nil {
				return nil, err
			}
			return struct {
				Deleted string `json:"deleted"`
			}{Deleted: in.Key}, nil
		},
	},
	{
		Name:        "activities_list",
		Description: "List the activities of a client, of one deal or of one lead, newest first: calls, meetings and notes.",
		Schema: objectSchema(map[string]any{
			"client_id": integerProp("Client id"),
			"deal_id":   integerProp("Deal id"),
			"lead_id":   integerProp("Lead id"),
		}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ClientID int64 `json:"client_id"`
				DealID   int64 `json:"deal_id"`
				LeadID   int64 `json:"lead_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			activities, err := s.services.CRM.Activities(c.ctx, appmodel.CRMActivityListQuery{
				TeamID: c.teamID, CallerID: c.userID, ClientID: in.ClientID, DealID: in.DealID, LeadID: in.LeadID,
			})
			if err != nil {
				return nil, err
			}
			out := make([]crmActivityView, 0, len(activities))
			for _, a := range activities {
				out = append(out, crmActivityViewIn(a, c.now.Location()))
			}
			return crmActivitiesResponse{Activities: out}, nil
		},
	},
	{
		Name: "activity_create",
		Description: "Log a call, a meeting or a note on a client, on a deal or on a lead. Give one of client_id, deal_id or lead_id, and a body or a result. " +
			"The kind defaults to note, and occurred_at defaults to now.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"client_id":   integerProp("Client id; a deal's client is used when left out"),
			"deal_id":     integerProp("Deal id"),
			"lead_id":     integerProp("Lead id; use it alone, without client_id or deal_id"),
			"kind":        enumProp("Kind of activity", "call", "meeting", "note"),
			"body":        stringProp("What happened"),
			"result":      stringProp("Outcome or the next agreed step"),
			"occurred_at": dateTimeProp("When it happened"),
		}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[crmActivityBody](args)
			if err != nil {
				return nil, err
			}
			activity, err := s.services.CRM.LogActivity(c.ctx, appmodel.CRMActivityCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, ClientID: in.ClientID, DealID: in.DealID, LeadID: in.LeadID, Kind: in.Kind,
				Body: in.Body, Result: in.Result, OccurredAt: in.OccurredAt,
			})
			if err != nil {
				return nil, err
			}
			return crmActivityViewIn(activity, c.now.Location()), nil
		},
	},
	{
		Name:        "calendar_events_list",
		Description: "List the team calendar events that overlap a time window. Times are RFC 3339 and the window is at most a year long. Only the events the token's user may see come back.",
		Schema: objectSchema(map[string]any{
			"from": dateTimeProp("Start of the window"),
			"to":   dateTimeProp("End of the window, after from"),
		}, "from", "to"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				From time.Time `json:"from"`
				To   time.Time `json:"to"`
			}](args)
			if err != nil {
				return nil, err
			}
			events, err := s.services.Calendar.Agenda(c.ctx, appmodel.CalendarEventListQuery{
				TeamID: c.teamID, CallerID: c.userID, From: in.From, To: in.To,
			})
			if err != nil {
				return nil, err
			}
			return calendarEventsResponse{Events: calendarEventViewsIn(events, c.now.Location())}, nil
		},
	},
	{
		Name: "calendar_event_create",
		Description: "Create a team calendar event. A shared event is visible to the whole team; otherwise it is visible to its author, " +
			"its participants and the managers. Participants must be members of the workspace.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"title":           stringProp("Event title"),
			"starts_at":       localDateTimeProp("Start"),
			"ends_at":         localDateTimeProp("End, after the start"),
			"location":        stringProp("Place or link"),
			"notes":           stringProp("Notes"),
			"project_id":      integerProp("Linked project id"),
			"client_id":       integerProp("Linked client id"),
			"shared":          booleanProp("Visible to the whole team"),
			"participant_ids": integerListProp("User ids of the participants"),
		}, "title", "starts_at", "ends_at"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[calendarEventBody](args)
			if err != nil {
				return nil, err
			}
			loc := c.now.Location()
			startsAt, err := calendarMoment("starts_at", in.StartsAt, loc)
			if err != nil {
				return nil, err
			}
			endsAt, err := calendarMoment("ends_at", in.EndsAt, loc)
			if err != nil {
				return nil, err
			}
			event, err := s.services.Calendar.Create(c.ctx, appmodel.CalendarEventCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, Title: in.Title, StartsAt: startsAt, EndsAt: endsAt,
				Location: in.Location, Notes: in.Notes, ProjectID: in.ProjectID, ClientID: in.ClientID,
				Shared: in.Shared, ParticipantIDs: in.ParticipantIDs,
			})
			if err != nil {
				return nil, err
			}
			return calendarEventViewIn(event, loc), nil
		},
	},
	{
		Name:        "calendar_event_update",
		Description: "Change the fields of a calendar event that are given. Only its author or a manager can change it. Participant ids replace the list when given.",
		Write:       true,
		Schema: objectSchema(map[string]any{
			"event_id":        integerProp("Event id"),
			"title":           stringProp("Event title"),
			"starts_at":       localDateTimeProp("Start"),
			"ends_at":         localDateTimeProp("End, after the start"),
			"location":        stringProp("Place or link"),
			"notes":           stringProp("Notes"),
			"project_id":      integerProp("Linked project id"),
			"client_id":       integerProp("Linked client id"),
			"shared":          booleanProp("Visible to the whole team"),
			"participant_ids": integerListProp("User ids of the participants"),
		}, "event_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				EventID int64 `json:"event_id"`
				calendarEventUpdateBody
			}](args)
			if err != nil {
				return nil, err
			}
			loc := c.now.Location()
			startsAt, err := optionalCalendarMoment("starts_at", in.StartsAt, loc)
			if err != nil {
				return nil, err
			}
			endsAt, err := optionalCalendarMoment("ends_at", in.EndsAt, loc)
			if err != nil {
				return nil, err
			}
			event, err := s.services.Calendar.Update(c.ctx, appmodel.CalendarEventUpdateRequest{
				TeamID: c.teamID, CallerID: c.userID, EventID: in.EventID, Title: in.Title, StartsAt: startsAt,
				EndsAt: endsAt, Location: in.Location, Notes: in.Notes, ProjectID: in.ProjectID,
				ClientID: in.ClientID, Shared: in.Shared, ParticipantIDs: in.ParticipantIDs,
			})
			if err != nil {
				return nil, err
			}
			return calendarEventViewIn(event, loc), nil
		},
	},
	{
		Name:        "calendar_event_delete",
		Description: "Delete a calendar event. Only its author or a manager can delete it, and the deletion cannot be undone.",
		Write:       true,
		Destructive: true,
		Schema:      objectSchema(map[string]any{"event_id": integerProp("Event id")}, "event_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				EventID int64 `json:"event_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			if err := s.services.Calendar.Delete(c.ctx, appmodel.CalendarEventDeleteRequest{
				TeamID: c.teamID, CallerID: c.userID, EventID: in.EventID,
			}); err != nil {
				return nil, err
			}
			return struct {
				Deleted int64 `json:"deleted"`
			}{Deleted: in.EventID}, nil
		},
	},
}

func crmContactViews(contacts []model.CRMContact) []crmContactView {
	out := make([]crmContactView, 0, len(contacts))
	for _, c := range contacts {
		out = append(out, crmContactViewOf(c))
	}
	return out
}

func crmDealViews(deals []model.CRMDeal, today string) []crmDealView {
	out := make([]crmDealView, 0, len(deals))
	for _, d := range deals {
		out = append(out, crmDealViewOf(d, today))
	}
	return out
}
