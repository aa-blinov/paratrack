package web

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// mcpLeadTools is the lead set of the CRM section (CRM-14). Each tool calls the workflow the
// HTTP route calls. The intake key is not a tool: a person makes it in the app, and the
// website never talks to the agent endpoint.
var mcpLeadTools = []mcpTool{
	{
		Name: "lead_list",
		Description: "List the leads of the workspace, newest first: company, contact, source, owner, status and next step. " +
			"Filter by status or owner. Every member reads the leads.",
		Schema: objectSchema(map[string]any{
			"status":   enumProp("Only leads in this status", model.LeadStatuses...),
			"owner_id": integerProp("Only leads of this owner"),
		}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Status  string `json:"status"`
				OwnerID int64  `json:"owner_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			leads, err := s.services.CRM.Leads(c.ctx, appmodel.CRMLeadListQuery{
				TeamID: c.teamID, CallerID: c.userID, Status: in.Status, OwnerID: in.OwnerID,
			})
			if err != nil {
				return nil, err
			}
			return crmLeadsResponse{Leads: crmLeadViews(leads, c.now)}, nil
		},
	},
	{
		Name: "lead_create",
		Description: "Record a lead: an enquiry that is not a client yet. Give at least one of company, contact_name, phone or email. " +
			"The status starts as new and the owner defaults to the token's user. Look leads up with lead_list first, so one enquiry is not recorded twice.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"company":      stringProp("Company name"),
			"contact_name": stringProp("Name of the person who asked"),
			"phone":        stringProp("Phone"),
			"email":        stringProp("Email"),
			"source":       stringProp("Where the lead came from, for example website or referral"),
			"message":      stringProp("What they asked for"),
			"owner_id":     integerProp("Owner user id; another owner needs a manager"),
			"next_step_at": dateProp("Date of the next step"),
		}),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[crmLeadBody](args)
			if err != nil {
				return nil, err
			}
			lead, err := s.services.CRM.CreateLead(c.ctx, appmodel.CRMLeadCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, OwnerID: in.OwnerID, Company: in.Company,
				ContactName: in.ContactName, Phone: in.Phone, Email: in.Email, Source: in.Source,
				Message: in.Message, NextStepAt: in.NextStepAt,
			})
			if err != nil {
				return nil, err
			}
			return crmLeadViewOf(lead, c.now), nil
		},
	},
	{
		Name: "lead_update",
		Description: "Change the details of a lead that are given; a field that is left out stays as it is. Any member may edit a lead. " +
			"Only a manager changes the owner. The lead still needs a company, a contact name, a phone or an email.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"lead_id":      integerProp("Lead id"),
			"company":      stringProp("Company name"),
			"contact_name": stringProp("Name of the person who asked"),
			"phone":        stringProp("Phone"),
			"email":        stringProp("Email"),
			"source":       stringProp("Where the lead came from"),
			"message":      stringProp("What they asked for"),
			"owner_id":     integerProp("Owner user id; changing it needs a manager"),
			"next_step_at": dateProp("Date of the next step; empty clears it"),
		}, "lead_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				LeadID int64 `json:"lead_id"`
				crmLeadUpdateBody
			}](args)
			if err != nil {
				return nil, err
			}
			lead, err := s.services.CRM.UpdateLead(c.ctx, appmodel.CRMLeadUpdateRequest{
				TeamID: c.teamID, CallerID: c.userID, LeadID: in.LeadID, OwnerID: in.OwnerID,
				Company: in.Company, ContactName: in.ContactName, Phone: in.Phone, Email: in.Email,
				Source: in.Source, Message: in.Message, NextStepAt: in.NextStepAt,
			})
			if err != nil {
				return nil, err
			}
			return crmLeadViewOf(lead, c.now), nil
		},
	},
	{
		Name: "lead_status_set",
		Description: "Move a lead to new, contacted, qualified or lost. A lost lead needs a lost_reason, and leaving lost clears it. " +
			"Only the lead's owner or a manager can move it. A lead becomes converted only through lead_convert.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"lead_id":     integerProp("Lead id"),
			"status":      enumProp("New status", model.LeadStatusNew, model.LeadStatusContacted, model.LeadStatusQualified, model.LeadStatusLost),
			"lost_reason": stringProp("Why the lead is lost; required when the status is lost"),
		}, "lead_id", "status"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				LeadID int64 `json:"lead_id"`
				crmLeadStatusBody
			}](args)
			if err != nil {
				return nil, err
			}
			lead, err := s.services.CRM.MoveLeadStatus(c.ctx, appmodel.CRMLeadStatusRequest{
				TeamID: c.teamID, CallerID: c.userID, LeadID: in.LeadID, Status: in.Status, LostReason: in.LostReason,
			})
			if err != nil {
				return nil, err
			}
			return crmLeadViewOf(lead, c.now), nil
		},
	},
	{
		Name: "lead_convert",
		Description: "Turn a lead into a client and a deal in the first open stage, and into a contact when the lead has a contact name. " +
			"The lead must be new, contacted or qualified, and only its owner or a manager can convert it. The client takes the lead's email, " +
			"and the lead's message becomes the deal's first note. The lead then keeps the ids of the client and the deal. " +
			"A client with the same name must not exist yet.",
		Write:  true,
		Schema: objectSchema(map[string]any{"lead_id": integerProp("Lead id")}, "lead_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				LeadID int64 `json:"lead_id"`
			}](args)
			if err != nil {
				return nil, err
			}
			lead, err := s.services.CRM.ConvertLead(c.ctx, appmodel.CRMLeadConvertRequest{
				TeamID: c.teamID, CallerID: c.userID, LeadID: in.LeadID,
			})
			if err != nil {
				return nil, err
			}
			return crmLeadViewOf(lead, c.now), nil
		},
	},
	{
		Name: "lead_activity_add",
		Description: "Log a call, a meeting or a note on a lead. Read the timeline of a lead with activities_list and its lead_id. " +
			"The kind defaults to note, and occurred_at defaults to now.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"lead_id":     integerProp("Lead id"),
			"kind":        enumProp("Kind of activity", "call", "meeting", "note"),
			"body":        stringProp("What happened"),
			"result":      stringProp("Outcome or the next agreed step"),
			"occurred_at": dateTimeProp("When it happened"),
		}, "lead_id"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				LeadID     int64     `json:"lead_id"`
				Kind       string    `json:"kind"`
				Body       string    `json:"body"`
				Result     string    `json:"result"`
				OccurredAt time.Time `json:"occurred_at"`
			}](args)
			if err != nil {
				return nil, err
			}
			activity, err := s.services.CRM.LogActivity(c.ctx, appmodel.CRMActivityCreateRequest{
				TeamID: c.teamID, CallerID: c.userID, LeadID: in.LeadID, Kind: in.Kind,
				Body: in.Body, Result: in.Result, OccurredAt: in.OccurredAt,
			})
			if err != nil {
				return nil, err
			}
			return crmActivityViewIn(activity, c.now.Location()), nil
		},
	},
	{
		Name: "lead_response_stats",
		Description: fmt.Sprintf("Measure how fast leads were answered. For the leads created from one day to another (YYYY-MM-DD, both days "+
			"included, in the workspace zone) it returns how many were created, how many got an activity at all, how many got it within %d "+
			"minutes of creation, and the median minutes from creation to the first activity (null while none has one). Every member reads it.",
			model.LeadResponseTargetMinutes),
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
			stats, err := s.services.CRM.LeadResponseStats(c.ctx, appmodel.CRMLeadResponseStatsQuery{
				TeamID: c.teamID, CallerID: c.userID, From: start, To: end,
			})
			if err != nil {
				return nil, err
			}
			return crmLeadResponseStatsViewOf(stats, in.From, in.To), nil
		},
	},
}
