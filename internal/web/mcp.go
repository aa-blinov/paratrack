package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/mcpwire"
	"github.com/aa-blinov/paratrack/internal/model"
)

// maxMCPBody bounds one JSON-RPC message. Tool arguments are small, and results
// go out, never in.
const maxMCPBody = 1 << 20

// mcpInstructions is read once at initialize, before any tool description.
const mcpInstructions = "paratrack: time tracking, clients and CRM (contacts, deals, activities), " +
	"the team calendar, projects, tasks, time sessions, goals, tags and the weekly plan. " +
	"A manager can read invoices and pay runs, draft and issue invoices, send them and record payments, " +
	"draft and pay payroll runs, and approve a timesheet week. Sending and paying reach the client and the payees. " +
	"Look up clients and deals before you create them. " +
	"Every call runs as the owner of the API token, with that person's rights. The server offers no delete " +
	"for clients or deals. An invoice, a pay run, a reopened week and the workspace are only requested, through the " +
	"*_request tools: a person confirms or rejects each request in the app, and the server never runs one itself. " +
	"A calendar event can be deleted by its author or a manager, a goal by " +
	"a manager, a time session by the person who tracked it, and a checklist step by a workspace member."

// mcpCall is what a tool needs from one request: the caller, the workspace, the
// workspace day and clock, and the read-only flag of the token.
type mcpCall struct {
	req      *http.Request
	ctx      context.Context
	teamID   int64
	userID   int64
	manage   bool
	readOnly bool
	today    string
	now      time.Time
	lang     i18n.Lang
}

// errBadArguments wraps a tool argument the server cannot read, so the agent sees
// which call was malformed.
var errBadArguments = errors.New("bad tool arguments")

type mcpInitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      mcpServerInfo  `json:"serverInfo"`
	Instructions    string         `json:"instructions"`
}

type mcpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// handleMCP serves the Model Context Protocol over HTTP. Each POST carries one
// JSON-RPC message and gets one JSON answer; a notification gets 202 and no body.
// The route is registered behind mcpBearerOnly, so the token is the only way in.
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMCPBody))
	if err != nil {
		s.writeJSON(w, mcpMessage{mcpwire.NewError(nil, mcpwire.CodeInvalidRequest, "request body is too large")})
		return
	}
	req, perr := mcpwire.Parse(body)
	if perr != nil {
		s.writeJSON(w, mcpMessage{mcpwire.Response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: perr}})
		return
	}
	if req.IsNotification() {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	s.writeJSON(w, mcpMessage{s.dispatchMCP(r, req)})
}

// mcpMessage is one JSON-RPC answer, written by the shared response encoder.
type mcpMessage struct {
	mcpwire.Response
}

func (mcpMessage) isJSONResponse() {}

func (s *Server) dispatchMCP(r *http.Request, req mcpwire.Request) mcpwire.Response {
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				return mcpwire.NewError(req.ID, mcpwire.CodeInvalidParams, "initialize params are not valid")
			}
		}
		return mcpwire.NewResult(req.ID, mcpInitializeResult{
			ProtocolVersion: mcpwire.Negotiate(params.ProtocolVersion),
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ServerInfo:      mcpServerInfo{Name: "paratrack", Version: s.appVersion},
			Instructions:    mcpInstructions,
		})
	case "ping":
		return mcpwire.NewResult(req.ID, struct{}{})
	case "tools/list":
		return mcpwire.NewResult(req.ID, map[string]any{"tools": mcpToolDescriptors(s.teamModules(r))})
	case "tools/call":
		return s.callMCPTool(r, req)
	default:
		return mcpwire.NewError(req.ID, mcpwire.CodeMethodNotFound, "method not found: "+req.Method)
	}
}

// callMCPTool runs one tool. A tool that fails returns a result with IsError set,
// so the model can correct the call. Only an unknown tool or a malformed call is a
// protocol error.
func (s *Server) callMCPTool(r *http.Request, req mcpwire.Request) mcpwire.Response {
	var params mcpToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		return mcpwire.NewError(req.ID, mcpwire.CodeInvalidParams, "tools/call needs a tool name")
	}
	tool, ok := mcpToolNamed(params.Name)
	if !ok {
		return mcpwire.NewError(req.ID, mcpwire.CodeInvalidParams, "unknown tool: "+params.Name)
	}
	call := mcpCall{
		req: r, ctx: r.Context(), teamID: teamID(r), userID: authenticatedUserID(r), manage: canManage(r),
		readOnly: readOnlyCredential(r.Context()), today: workspaceToday(r), now: userNow(r), lang: resolveLang(r),
	}
	if section := mcpToolSections[tool.Name]; section != "" && !s.teamModules(r)[section] {
		key := "mod.off"
		if !call.manage {
			key = "mod.offMember"
		}
		return mcpwire.NewResult(req.ID, mcpwire.TextResult(i18n.T(call.lang, key), true))
	}
	if tool.Write && call.readOnly {
		return mcpwire.NewResult(req.ID, mcpwire.TextResult(i18n.T(call.lang, "mcp.readOnly"), true))
	}
	args := params.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	value, err := tool.Run(s, call, args)
	if err != nil {
		return mcpwire.NewResult(req.ID, mcpwire.TextResult(s.mcpErrorText(call, err), true))
	}
	if tool.Write {
		s.audit(r, "mcp.write", tool.Name, "агентом через MCP")
	}
	text, err := json.Marshal(value)
	if err != nil {
		s.logInternalError(err)
		return mcpwire.NewResult(req.ID, mcpwire.TextResult("internal server error", true))
	}
	return mcpwire.NewResult(req.ID, mcpwire.TextResult(string(text), false))
}

// mcpErrorText is the text the agent reads when a tool fails. It is written in the
// language of the token owner, and an input problem names the field.
func (s *Server) mcpErrorText(call mcpCall, err error) string {
	if field, ok := crmInputField(err); ok {
		return i18n.T(call.lang, "err.invalidInput") + " " + field
	}
	if errors.Is(err, errBadArguments) {
		return i18n.T(call.lang, "err.invalidInput") + ": " + err.Error()
	}
	switch {
	case errors.Is(err, model.ErrForbidden):
		return i18n.T(call.lang, "err.forbiddenAction")
	case errors.Is(err, model.ErrProjectManagerRequired):
		return i18n.T(call.lang, "crm.projectManagerOnly")
	case errors.Is(err, model.ErrNotFound):
		return i18n.T(call.lang, "err.notFoundEntity")
	}
	if text, ok := s.mcpMoneyErrorText(call, err); ok {
		return text
	}
	if text, ok := mcpWorkErrorText(call, err); ok {
		return text
	}
	s.logInternalError(err)
	return "internal server error"
}

// ctxReadOnlyCredentialKey marks a request that authenticated with a read-only token.
type ctxReadOnlyCredentialKey struct{}

// readOnlyCredential reports that the request authenticated with a read-only API
// token. Its write tools are refused in the call, because /mcp is a POST either way.
func readOnlyCredential(ctx context.Context) bool {
	ro, _ := ctx.Value(ctxReadOnlyCredentialKey{}).(bool)
	return ro
}

// mcpBearerOnly admits a request only with an API token in the Authorization
// header. A browser cookie is never accepted here, so a page on another site
// cannot drive the agent endpoint with the person's session.
func mcpBearerOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer pt_") {
			w.Header().Set("WWW-Authenticate", `Bearer realm="paratrack"`)
			apiUnauthorized(w, r)
			return
		}
		next(w, r)
	}
}
