package web

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"

	"github.com/aa-blinov/paratrack/internal/mcpwire"
)

// modernMeta is the _meta that a 2026-07-28 client puts on every request.
const modernMeta = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`

// modernHeaders is what the same client sends beside a body with this method and tool.
func modernHeaders(token, method, tool string) map[string]string {
	headers := mcpBearer(token)
	headers["MCP-Protocol-Version"] = mcpwire.ModernVersion
	headers["Mcp-Method"] = method
	if tool != "" {
		headers["Mcp-Name"] = tool
	}
	return headers
}

// assertMCPRejected checks the HTTP status and the JSON-RPC code of a refused message.
func assertMCPRejected(t *testing.T, e *apiEnv, headers map[string]string, body string, status, code int) {
	t.Helper()
	got, out := postMCP(t, e, headers, body, false)
	errObj, _ := out["error"].(map[string]any)
	if got != status || errObj["code"] != float64(code) {
		t.Fatalf("refused message = %d %v, want %d and code %d", got, out, status, code)
	}
}

func TestMCPModernRequestsRepeatTheirMethodAndToolInHeaders(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-modern-headers@x.test")
	token := apiTokenOf(t, e, "agent")
	list := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{%s}}`, modernMeta)
	call := fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"projects_list","arguments":{},%s}}`, modernMeta)

	status, out := postMCP(t, e, modernHeaders(token, "tools/list", ""), list, false)
	result, _ := out["result"].(map[string]any)
	if status != http.StatusOK || result["resultType"] != "complete" || result["cacheScope"] != "private" || result["ttlMs"] != float64(0) {
		t.Fatalf("modern tools/list = %d %v", status, out)
	}

	noVersion := modernHeaders(token, "tools/list", "")
	delete(noVersion, "MCP-Protocol-Version")
	assertMCPRejected(t, e, noVersion, list, http.StatusBadRequest, mcpwire.CodeHeaderMismatch)
	assertMCPRejected(t, e, modernHeaders(token, "ping", ""), list, http.StatusBadRequest, mcpwire.CodeHeaderMismatch)
	assertMCPRejected(t, e, modernHeaders(token, "tools/call", ""), call, http.StatusBadRequest, mcpwire.CodeHeaderMismatch)
	assertMCPRejected(t, e, modernHeaders(token, "tools/call", "lead_list"), call, http.StatusBadRequest, mcpwire.CodeHeaderMismatch)

	status, out = postMCP(t, e, modernHeaders(token, "tools/call", "projects_list"), call, false)
	result, _ = out["result"].(map[string]any)
	if status != http.StatusOK || result["resultType"] != "complete" || out["error"] != nil {
		t.Fatalf("modern tools/call = %d %v", status, out)
	}

	encoded := "=?base64?" + base64.StdEncoding.EncodeToString([]byte("projects_list")) + "?="
	status, out = postMCP(t, e, modernHeaders(token, "tools/call", encoded), call, false)
	if status != http.StatusOK || out["error"] != nil {
		t.Fatalf("tools/call with the Base64 name = %d %v", status, out)
	}
}

func TestMCPUnsupportedVersionListsTheVersionsItServes(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-modern-version@x.test")
	token := apiTokenOf(t, e, "agent")
	for _, version := range []string{"1900-01-01", mcpwire.LegacyVersion} {
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":%q,"io.modelcontextprotocol/clientCapabilities":{}}}}`, version)
		headers := mcpBearer(token)
		headers["MCP-Protocol-Version"] = version
		headers["Mcp-Method"] = "tools/list"
		status, out := postMCP(t, e, headers, body, false)
		errObj, _ := out["error"].(map[string]any)
		data, _ := errObj["data"].(map[string]any)
		supported, _ := data["supported"].([]any)
		if status != http.StatusBadRequest || errObj["code"] != float64(mcpwire.CodeUnsupportedVersion) ||
			len(supported) != 1 || supported[0] != mcpwire.ModernVersion || data["requested"] != version {
			t.Fatalf("version %s = %d %v, want 400 and the supported list", version, status, out)
		}
	}
}

func TestMCPServerDiscoverAnswersForTheModernRevision(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-modern-discover@x.test")
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":"discover-1","method":"server/discover","params":{%s}}`, modernMeta)
	status, out := postMCP(t, e, modernHeaders(apiTokenOf(t, e, "agent"), "server/discover", ""), body, false)
	result, _ := out["result"].(map[string]any)
	versions, _ := result["supportedVersions"].([]any)
	meta, _ := result["_meta"].(map[string]any)
	identity, _ := meta["io.modelcontextprotocol/serverInfo"].(map[string]any)
	if status != http.StatusOK || out["id"] != "discover-1" || result["resultType"] != "complete" ||
		len(versions) != 1 || versions[0] != mcpwire.ModernVersion || identity["name"] != "paratrack" {
		t.Fatalf("server/discover = %d %v", status, out)
	}
}

func TestMCPModernUnknownMethodIsNotFound(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-modern-missing@x.test")
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":4,"method":"resources/list","params":{%s}}`, modernMeta)
	assertMCPRejected(t, e, modernHeaders(apiTokenOf(t, e, "agent"), "resources/list", ""), body, http.StatusNotFound, mcpwire.CodeMethodNotFound)
}

// A 2025-06-18 client sends no version in the request, and its answers keep the shape
// they had before revision 2026-07-28 was added.
func TestMCPLegacyRequestsKeepTheirShape(t *testing.T) {
	e := newAPIEnv(t)
	e.register("mcp-legacy-shape@x.test")
	auth := mcpBearer(apiTokenOf(t, e, "agent"))

	status, out := postMCP(t, e, auth, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, false)
	result, _ := out["result"].(map[string]any)
	if status != http.StatusOK || result["tools"] == nil || result["resultType"] != nil || result["ttlMs"] != nil {
		t.Fatalf("legacy tools/list = %d %v", status, out)
	}
	status, out = postMCP(t, e, auth, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"projects_list","arguments":{}}}`, false)
	result, _ = out["result"].(map[string]any)
	if status != http.StatusOK || result["content"] == nil || result["resultType"] != nil {
		t.Fatalf("legacy tools/call = %d %v", status, out)
	}

	// A body without a version in _meta cannot be read as 2026-07-28, whatever the header says.
	headers := mcpBearer(apiTokenOf(t, e, "agent"))
	headers["MCP-Protocol-Version"] = mcpwire.ModernVersion
	assertMCPRejected(t, e, headers, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, http.StatusBadRequest, mcpwire.CodeHeaderMismatch)
}
