package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// mcpTool is one tool an agent can call. Names follow entity_action (design D7).
// Descriptions are in English, because the agent reads them first. A write tool is
// refused for a read-only token.
type mcpTool struct {
	Name        string
	Description string
	Write       bool
	Destructive bool
	Schema      map[string]any
	Run         func(s *Server, c mcpCall, args json.RawMessage) (any, error)
}

// mcpToolSections names the workspace section each tool belongs to. A tool of a
// section that is switched off is hidden from the list and refused when called,
// as the screens of that section are.
var mcpToolSections = map[string]string{
	"contacts_list": "crm", "contact_create": "crm", "deals_list": "crm", "deal_create": "crm",
	"deal_update": "crm", "deal_move_stage": "crm", "deal_create_project": "crm", "activities_list": "crm", "activity_create": "crm",
	"deal_stages_list": "crm", "deal_stage_create": "crm", "deal_stage_update": "crm", "deal_stage_delete": "crm",
	"lead_list": "crm", "lead_create": "crm", "lead_update": "crm", "lead_status_set": "crm",
	"lead_convert": "crm", "lead_activity_add": "crm",
	"calendar_events_list": "calendar", "calendar_event_create": "calendar",
	"calendar_event_update": "calendar", "calendar_event_delete": "calendar",
	"tasks_list": "tasks", "task_get": "tasks", "task_create": "tasks", "task_set_status": "tasks",
	"task_update": "tasks", "task_comment_add": "tasks", "task_checklist_add": "tasks", "task_checklist_set_done": "tasks",
	"task_checklist_delete": "tasks", "task_dependency_add": "tasks", "task_dependency_remove": "tasks", "task_archive": "tasks",
	"goals_list": "goals", "goal_set": "goals", "goal_delete": "goals",
	"tags_list": "tags", "tag_create": "tags",
	"schedule_entries_list": "schedule", "schedule_cell_set": "schedule",
	"invoices_list": "invoices", "invoice_get": "invoices",
	"invoice_draft_create": "invoices", "invoice_mark_sent": "invoices", "invoice_send": "invoices",
	"invoice_record_payment": "invoices",
	"payroll_runs_list":      "payroll", "payroll_run_create": "payroll", "payroll_run_pay": "payroll",
	"invoice_delete_request": "invoices", "payroll_run_delete_request": "payroll",
	"integrations_list": "integrations",
}

// mcpTools is the whole tool set: CRM, leads and calendar, then the entities that existed
// before them, then goals, tags, the plan, invoices, payroll, the money writes, the
// project, task and time writes, and the reads of the workspace's administration.
var mcpTools = slices.Concat(mcpCRMTools, mcpLeadTools, mcpEntityTools, mcpMoreTools, mcpMoneyTools, mcpQueueTools, mcpWorkTools, mcpAdminTools)

func mcpToolNamed(name string) (mcpTool, bool) {
	for _, tool := range mcpTools {
		if tool.Name == name {
			return tool, true
		}
	}
	return mcpTool{}, false
}

type mcpToolDescriptor struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations mcpAnnotations `json:"annotations"`
}

type mcpAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
}

func mcpToolDescriptors(sections map[string]bool) []mcpToolDescriptor {
	out := make([]mcpToolDescriptor, 0, len(mcpTools))
	for _, tool := range mcpTools {
		if section := mcpToolSections[tool.Name]; section != "" && !sections[section] {
			continue
		}
		out = append(out, mcpToolDescriptor{
			Name: tool.Name, Description: tool.Description, InputSchema: tool.Schema,
			Annotations: mcpAnnotations{ReadOnlyHint: !tool.Write, DestructiveHint: tool.Destructive},
		})
	}
	return out
}

// decodeArgs reads tool arguments strictly. A misspelt field is an error the agent
// can see, not a field that silently does nothing.
func decodeArgs(args json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: %w", errBadArguments, err)
	}
	return nil
}

// mcpArgs decodes the arguments of one tool into its typed input.
func mcpArgs[T any](args json.RawMessage) (T, error) {
	var in T
	err := decodeArgs(args, &in)
	return in, err
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func integerProp(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func booleanProp(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func enumProp(description string, values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values, "description": description}
}

func dateProp(description string) map[string]any {
	return map[string]any{"type": "string", "format": "date", "description": description + " (YYYY-MM-DD)"}
}

func dateTimeProp(description string) map[string]any {
	return map[string]any{"type": "string", "format": "date-time", "description": description + " (RFC 3339)"}
}

// localDateTimeProp describes a moment written as a wall clock in the workspace
// zone, or as RFC 3339 with its own offset.
func localDateTimeProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description + ": 2026-10-26T10:00 in the workspace zone, or RFC 3339 with an offset"}
}

func integerListProp(description string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": description}
}
