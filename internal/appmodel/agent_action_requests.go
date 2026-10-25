package appmodel

import (
	"errors"
	"time"
)

// The confirmation queue (agent-action-queue). An agent requests one of these four
// actions; a person confirms or rejects it in the app. Writes end in Request, reads in Query.

const (
	AgentActionInvoiceDelete    = "invoice_delete"
	AgentActionPayrollRunDelete = "payroll_run_delete"
	AgentActionWeekReopen       = "week_reopen"
	AgentActionWorkspaceDelete  = "workspace_delete"
)

// AgentActionReasonMax is the longest reason a request may carry, in characters.
const AgentActionReasonMax = 500

// AgentActionClaimWindow is how long a confirmation holds its claim. A confirmation that
// stopped mid-run leaves its request pending; once the window passes, a new decision may
// take the request over. ponytail: fixed window, no lease renewal, so a run longer than
// this could be taken over while it still runs.
const AgentActionClaimWindow = 10 * time.Minute

var (
	// ErrAgentActionDecided refuses a decision on a request that already has one.
	ErrAgentActionDecided = errors.New("agent action is already decided")
	// ErrAgentActionClaimed refuses a second confirmation while the first one runs.
	ErrAgentActionClaimed = errors.New("agent action is being carried out")
	// ErrPayrollRunNotDraft refuses a request to delete a run that is no longer a draft.
	ErrPayrollRunNotDraft = errors.New("pay run is not a draft")
	// ErrAgentWeekNotApproved refuses a request to reopen a week nobody approved.
	ErrAgentWeekNotApproved = errors.New("week is not approved")
)

// AgentActionCreateRequest asks for one action. TargetID is the invoice or pay run;
// a week names its first day in WeekStart, and a workspace is its own target.
type AgentActionCreateRequest struct {
	TeamID    int64
	CallerID  int64
	Kind      string
	TargetID  int64
	WeekStart time.Time
	Reason    string
}

// AgentActionListQuery lists the requests still waiting for a decision.
type AgentActionListQuery struct {
	TeamID   int64
	CallerID int64
}

// AgentActionDecisionRequest confirms (Reject false) or rejects a pending request.
type AgentActionDecisionRequest struct {
	TeamID   int64
	CallerID int64
	ActionID int64
	Reject   bool
}

// AgentActionFinishRequest writes the outcome of a confirmed request. ClaimedAt is the
// decision time the confirmation returned, so only the claim that is running can finish.
type AgentActionFinishRequest struct {
	TeamID    int64
	CallerID  int64
	ActionID  int64
	ClaimedAt time.Time
	Status    string
	Error     string
}

// AgentActionView is one request as the queue shows it.
type AgentActionView struct {
	ID              int64
	TeamID          int64
	Kind            string
	TargetID        int64
	TargetLabel     string
	Reason          string
	Status          string
	RequestedBy     int64
	RequestedByName string
	CreatedAt       time.Time
	DecidedBy       int64
	DecidedAt       time.Time
}
