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
			"A deal is overdue when its next step date is before today. Filter by stage, owner or overdue.",
		Schema: objectSchema(map[string]any{
			"stage":    enumProp("Only deals in this stage", "new", "working", "proposal", "won", "lost"),
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
			return crmDealsResponse{Today: c.today, Deals: crmDealViews(deals, c.today), Pipeline: crmPipelineViewOf(pipeline)}, nil
		},
	},
	{
		Name: "deal_create",
		Description: "Create a deal. The stage defaults to new and the owner to the token's user. " +
			"Amounts are in cents of the deal currency, and probability is a percentage from 0 to 100.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"title":        stringProp("Deal title"),
			"stage":        enumProp("Stage", "new", "working", "proposal", "won", "lost"),
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
		Description: "Move a deal to another stage. A lost deal needs a lost_reason. Leaving the lost stage clears the reason. " +
			"Only the deal owner or a manager can move a deal.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"deal_id":     integerProp("Deal id"),
			"stage":       enumProp("New stage", "new", "working", "proposal", "won", "lost"),
			"lost_reason": stringProp("Why the deal is lost; required for the lost stage"),
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
		Name:        "activities_list",
		Description: "List the activities of a client or of one deal, newest first: calls, meetings and notes.",
		Schema: objectSchema(map[string]any{
			"client_id": integerProp("Client id"),
			"deal_id":   integerProp("Deal id"),
		}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				ClientID int64 `json:"client_id"`
				DealID   int64 `json:"deal_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			activities, err := s.services.CRM.Activities(c.ctx, appmodel.CRMActivityListQuery{
				TeamID: c.teamID, CallerID: c.userID, ClientID: in.ClientID, DealID: in.DealID,
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
		Description: "Log a call, a meeting or a note on a client or on a deal. Give a client_id or a deal_id, and a body or a result. " +
			"The kind defaults to note, and occurred_at defaults to now.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"client_id":   integerProp("Client id; a deal's client is used when left out"),
			"deal_id":     integerProp("Deal id"),
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
				TeamID: c.teamID, CallerID: c.userID, ClientID: in.ClientID, DealID: in.DealID, Kind: in.Kind,
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
