package web

import (
	"encoding/json"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// mcpQueueTools ask for the four actions an agent may not run itself: deleting an
// invoice or a pay run, reopening an approved week and deleting the workspace. Each call
// only queues a request. A person confirms or rejects it in the app, under Settings,
// Confirmations, so no tool here confirms, rejects or runs anything.
var mcpQueueTools = []mcpTool{
	{
		Name: "invoice_delete_request",
		Description: "Asks for a draft invoice to be deleted. Nothing is deleted by this call: the request waits until a person " +
			"confirms it in the app. Only a draft can be requested. Managers only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"invoice_id": integerProp("Invoice id"),
			"reason":     stringProp("Why the invoice should go, up to 500 characters"),
		}, "invoice_id", "reason"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				InvoiceID int64  `json:"invoice_id"`
				Reason    string `json:"reason"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			return s.mcpRequestAction(c, appmodel.AgentActionInvoiceDelete, in.InvoiceID, time.Time{}, in.Reason)
		},
	},
	{
		Name: "payroll_run_delete_request",
		Description: "Asks for a draft pay run to be deleted. Nothing is deleted by this call: the request waits until a person " +
			"confirms it in the app. Only a draft can be requested. Managers only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"run_id": integerProp("Pay run id"),
			"reason": stringProp("Why the pay run should go, up to 500 characters"),
		}, "run_id", "reason"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				RunID  int64  `json:"run_id"`
				Reason string `json:"reason"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			return s.mcpRequestAction(c, appmodel.AgentActionPayrollRunDelete, in.RunID, time.Time{}, in.Reason)
		},
	},
	{
		Name: "week_reopen_request",
		Description: "Asks for an approved timesheet week to be reopened, as the timesheet's reopen button does. Nothing reopens by " +
			"this call: a person confirms the request in the app. A pay run that covers the week stops the reopening when it is " +
			"confirmed. Managers only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"week_start": dateProp("First day of the approved week"),
			"reason":     stringProp("Why the week should be reopened, up to 500 characters"),
		}, "week_start", "reason"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				WeekStart string `json:"week_start"`
				Reason    string `json:"reason"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage {
				return nil, model.ErrForbidden
			}
			day, err := mcpLocalDate("week_start", in.WeekStart, c.now.Location())
			if err != nil {
				return nil, err
			}
			return s.mcpRequestAction(c, appmodel.AgentActionWeekReopen, 0, day, in.Reason)
		},
	},
	{
		Name: "workspace_delete_request",
		Description: "Asks for this workspace to be deleted. Nothing is deleted by this call. Only the owner can confirm the request, " +
			"in the app, and a workspace that still has other members fails when it is confirmed. Owner only.",
		Write: true,
		Schema: objectSchema(map[string]any{
			"reason": stringProp("Why the workspace should be deleted, up to 500 characters"),
		}, "reason"),
		Run: func(s *Server, c mcpCall, args json.RawMessage) (any, error) {
			in, err := mcpArgs[struct {
				Reason string `json:"reason"`
			}](args)
			if err != nil {
				return nil, err
			}
			if !c.manage || !RoleFrom(c.ctx).IsOwner() {
				return nil, model.ErrForbidden
			}
			return s.mcpRequestAction(c, appmodel.AgentActionWorkspaceDelete, c.teamID, time.Time{}, in.Reason)
		},
	},
}

// mcpRequestAction queues one request and answers with it, as the other write tools do.
func (s *Server) mcpRequestAction(c mcpCall, kind string, targetID int64, weekStart time.Time, reason string) (any, error) {
	view, err := s.services.AgentActions.Request(c.ctx, appmodel.AgentActionCreateRequest{
		TeamID: c.teamID, CallerID: c.userID, Kind: kind, TargetID: targetID, WeekStart: weekStart, Reason: reason,
	})
	if err != nil {
		return nil, err
	}
	return mcpAgentActionView{ID: view.ID, Kind: view.Kind, TargetLabel: view.TargetLabel, Status: view.Status}, nil
}

type mcpAgentActionView struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	TargetLabel string `json:"target_label"`
	Status      string `json:"status"`
}
