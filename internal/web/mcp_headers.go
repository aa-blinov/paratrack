package web

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/aa-blinov/paratrack/internal/mcpwire"
)

// mcpSupportedVersions are the revisions a request may declare in its own _meta. The
// legacy revision is spoken through initialize, so it is not listed here.
var mcpSupportedVersions = []string{mcpwire.ModernVersion}

// mcpRejection is a message the transport refuses before any method or tool runs.
type mcpRejection struct {
	status  int
	code    int
	message string
	data    any
}

// checkMCPHeaders applies revision 2026-07-28 to the HTTP transport. The version must
// be one the server implements, and the headers must repeat the body: a gateway that
// routes on the headers and the server that runs the body then see the same call.
func checkMCPHeaders(r *http.Request, req mcpwire.Request, version string, modern bool) *mcpRejection {
	if !modern {
		// A legacy body has no version to repeat, so a header that names 2026-07-28 is a mismatch.
		if r.Header.Get("MCP-Protocol-Version") == mcpwire.ModernVersion {
			return mcpHeaderMismatch("MCP-Protocol-Version header names 2026-07-28 but the request has no protocol version in _meta")
		}
		return nil
	}
	if !slices.Contains(mcpSupportedVersions, version) {
		return &mcpRejection{
			status:  http.StatusBadRequest,
			code:    mcpwire.CodeUnsupportedVersion,
			message: "unsupported protocol version " + version,
			data:    map[string]any{"supported": mcpSupportedVersions, "requested": version},
		}
	}
	if r.Header.Get("MCP-Protocol-Version") != version {
		return mcpHeaderMismatch("MCP-Protocol-Version header does not match the protocol version in the request")
	}
	if r.Header.Get("Mcp-Method") != req.Method {
		return mcpHeaderMismatch("Mcp-Method header does not match method " + req.Method)
	}
	if req.Method == "tools/call" {
		name := mcpCallToolName(req.Params)
		header, ok := mcpwire.HeaderValue(r.Header.Get("Mcp-Name"))
		if !ok || name == "" || header != name {
			return mcpHeaderMismatch("Mcp-Name header does not match the tool name in the request")
		}
	}
	return nil
}

func mcpHeaderMismatch(message string) *mcpRejection {
	return &mcpRejection{
		status:  http.StatusBadRequest,
		code:    mcpwire.CodeHeaderMismatch,
		message: "header mismatch: " + message,
	}
}

// mcpCallToolName is the tool a tools/call names in its body. An unreadable body names
// no tool, which the header check then refuses.
func mcpCallToolName(params json.RawMessage) string {
	var call mcpToolCallParams
	if json.Unmarshal(params, &call) != nil {
		return ""
	}
	return call.Name
}
