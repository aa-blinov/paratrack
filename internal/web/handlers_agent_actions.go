package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The confirmation page lists what agents asked for and lets a manager confirm or reject
// each request. A confirmation runs the same service the page runs for that kind, as the
// person who confirms it, so the workflow's own rules still decide the outcome.

// personOnly admits a browser session and nothing else. An API token is the agent's
// credential, and a confirmation is the person's own act, so a token never reaches the
// handler, whatever role it carries.
func personOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, i18n.T(resolveLang(r), "err.forbiddenAction"), http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

type agentActionsPage struct {
	pageData
	AgentActionsReact bool
	Items             []agentActionRow
	Flash             string
	FlashOK           bool
}

func (p *agentActionsPage) setCSRF(t string) { p.pageData.setCSRF(t) }

type agentActionRow struct {
	ID          int64
	Kind        string
	Target      string
	Reason      string
	RequestedBy string
	When        string
	// CanDecide is false for a workspace deletion the viewer is not the owner of.
	CanDecide bool
	// Claimed is true while a confirmation of this request is still running.
	Claimed bool
}

func (s *Server) handleAgentActionsPage(w http.ResponseWriter, r *http.Request) {
	pending, err := s.services.AgentActions.Pending(r.Context(), appmodel.AgentActionListQuery{TeamID: teamID(r), CallerID: authenticatedUserID(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	lang := resolveLang(r)
	loc := userLoc(r)
	now := userNow(r)
	owner := RoleFrom(r.Context()).IsOwner()
	data := agentActionsPage{
		pageData:          pageData{Title: "Confirmations", Active: "settings-agent-actions", Lang: string(lang), ReactApp: true},
		AgentActionsReact: true,
		Items:             make([]agentActionRow, 0, len(pending)),
	}
	for _, p := range pending {
		data.Items = append(data.Items, agentActionRow{
			ID: p.ID, Kind: p.Kind, Target: p.TargetLabel, Reason: p.Reason, RequestedBy: p.RequestedByName,
			When:      fmtWhen(lang, p.CreatedAt.In(loc), now),
			CanDecide: p.Kind != appmodel.AgentActionWorkspaceDelete || owner,
			Claimed:   !p.DecidedAt.IsZero() && now.Sub(p.DecidedAt) < appmodel.AgentActionClaimWindow,
		})
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, lang)
	}
	s.renderPageForRequest(w, r, "Confirmations", "settings-agent-actions", &data)
}

func (s *Server) handleAgentActionConfirm(w http.ResponseWriter, r *http.Request) {
	s.decideAgentAction(w, r, false)
}

func (s *Server) handleAgentActionReject(w http.ResponseWriter, r *http.Request) {
	s.decideAgentAction(w, r, true)
}

// decideAgentAction confirms or rejects one request. A rejection runs nothing. A
// confirmation claims the request, runs the action and records the outcome, so a refused
// action stays visible as failed with the reason, not as a request that vanished.
func (s *Server) decideAgentAction(w http.ResponseWriter, r *http.Request, reject bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	team, user := teamID(r), authenticatedUserID(r)
	action, err := s.services.AgentActions.Decide(ctx, appmodel.AgentActionDecisionRequest{TeamID: team, CallerID: user, ActionID: id, Reject: reject})
	if err != nil {
		s.agentActionBack(w, r, false, s.agentActionRefusal(r, err))
		return
	}
	if reject {
		s.audit(r, "agent.reject", action.TargetLabel, action.Kind)
		s.agentActionBack(w, r, true, "agent.rejected")
		return
	}
	remaining, runErr := s.runAgentAction(r, action)
	if action.Kind == appmodel.AgentActionWorkspaceDelete && runErr == nil {
		// The workspace and its queue went with it, so there is nothing left to finish.
		s.audit(r, "agent.confirm", action.TargetLabel, "done")
		s.leaveDeletedWorkspace(w, r, user, remaining)
		return
	}
	outcome := appmodel.AgentActionFinishRequest{TeamID: team, CallerID: user, ActionID: id, ClaimedAt: action.DecidedAt, Status: "done"}
	if runErr != nil {
		outcome.Status, outcome.Error = "failed", s.agentActionErrorText(r, action.Kind, runErr)
	}
	if err := s.services.AgentActions.Finish(ctx, outcome); err != nil {
		s.logInternalError(err)
	}
	s.audit(r, "agent.confirm", action.TargetLabel, outcome.Status)
	if runErr != nil {
		s.agentActionBack(w, r, false, outcome.Error)
		return
	}
	s.agentActionBack(w, r, true, "agent.confirmed")
}

// runAgentAction carries out a confirmed request with the service the page uses for its
// kind. A week is reopened only under the exact approval the request named, so a week
// approved again since is not mistaken for it.
func (s *Server) runAgentAction(r *http.Request, action appmodel.AgentActionView) ([]int64, error) {
	ctx := r.Context()
	team, user := teamID(r), authenticatedUserID(r)
	switch action.Kind {
	case appmodel.AgentActionInvoiceDelete:
		return nil, s.services.Invoicing.Drafts.DeleteDraft(ctx, appmodel.InvoiceMutationRequest{TeamID: team, InvoiceID: action.TargetID, CallerID: user})
	case appmodel.AgentActionPayrollRunDelete:
		return nil, s.services.Payroll.DeleteDraft(ctx, appmodel.PayrollMutationRequest{TeamID: team, RunID: action.TargetID, CallerID: user})
	case appmodel.AgentActionWeekReopen:
		start := time.Unix(action.TargetID, 0)
		approvals, err := s.services.Tracking.Queries.TimesheetApprovals(ctx, team, start, start.Add(time.Second))
		if err != nil {
			return nil, err
		}
		for _, approval := range approvals {
			if approval.Start.Equal(start) {
				return nil, s.services.Tracking.Commands.ReopenWeek(ctx, appmodel.TimesheetApprovalRequest{
					TeamID: team, CallerID: user, Start: approval.Start, End: approval.End,
				})
			}
		}
		return nil, model.ErrNotFound
	case appmodel.AgentActionWorkspaceDelete:
		return s.services.TeamOps.DeleteWorkspace(ctx, appmodel.WorkspaceDeleteRequest{TeamID: team, CallerID: user})
	}
	return nil, appmodel.FieldError{Field: "kind"}
}

// agentActionRefusal is the sentence a refused decision shows.
func (s *Server) agentActionRefusal(r *http.Request, err error) string {
	lang := resolveLang(r)
	switch {
	case errors.Is(err, appmodel.ErrAgentActionDecided):
		return i18n.T(lang, "agent.decided")
	case errors.Is(err, appmodel.ErrAgentActionClaimed):
		return i18n.T(lang, "agent.inProgress")
	case errors.Is(err, model.ErrForbidden):
		return i18n.T(lang, "err.forbiddenAction")
	case errors.Is(err, model.ErrNotFound):
		return i18n.T(lang, "err.notFoundEntity")
	}
	s.logInternalError(err)
	return i18n.T(lang, "err.internal")
}

// agentActionErrorText is the sentence a failed confirmation stores and shows. It uses
// the words the page of that action uses for the same refusal.
func (s *Server) agentActionErrorText(r *http.Request, kind string, err error) string {
	lang := resolveLang(r)
	switch {
	case errors.Is(err, model.ErrNotFound):
		return i18n.T(lang, "err.notFoundEntity")
	case errors.Is(err, model.ErrForbidden):
		return i18n.T(lang, "err.forbiddenAction")
	}
	switch kind {
	case appmodel.AgentActionInvoiceDelete:
		return s.invoiceErrorMessage(r, err)
	case appmodel.AgentActionPayrollRunDelete:
		return s.payrollErrorMessage(r, err)
	case appmodel.AgentActionWeekReopen:
		return approvalMessage(lang, err)
	}
	return s.teamErrorMessage(r, err)
}

func (s *Server) agentActionBack(w http.ResponseWriter, r *http.Request, ok bool, msg string) {
	http.Redirect(w, r, "/settings/agent-actions?flash="+encodeFlash(ok, msg), http.StatusSeeOther)
}
