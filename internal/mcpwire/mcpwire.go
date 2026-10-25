// Package mcpwire holds the JSON-RPC 2.0 messages of the Model Context Protocol
// that paratrack speaks over HTTP. It knows nothing about the product: tools and
// their rules live in the transport adapter.
package mcpwire

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// LegacyVersion is the revision initialize answers with. Its requests carry no version
// of their own, and batching was removed in it, so one request is one JSON object.
const LegacyVersion = "2025-06-18"

// ModernVersion is the revision that puts the version, the client and its capabilities
// in every request's _meta, with no initialize handshake and no session.
const ModernVersion = "2026-07-28"

// Negotiate returns the revision to answer initialize with: the client's own when the
// server speaks it, otherwise the newest legacy revision the server speaks.
func Negotiate(requested string) string {
	if requested == LegacyVersion {
		return requested
	}
	return LegacyVersion
}

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// The specification keeps -32020 to -32099 for its own errors.
const (
	CodeHeaderMismatch     = -32020
	CodeUnsupportedVersion = -32022
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports a message without an id. The server never answers those.
func (r Request) IsNotification() bool {
	return len(r.ID) == 0
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Parse decodes one request. A malformed message or a batch is a protocol error,
// and the caller answers it with the returned Error and a null id.
func Parse(body []byte) (Request, *Error) {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 {
		return Request{}, &Error{Code: CodeInvalidRequest, Message: "empty request body"}
	}
	if trimmed[0] == '[' {
		return Request{}, &Error{Code: CodeInvalidRequest, Message: "batches are not supported, send one message per request"}
	}
	var req Request
	if err := json.Unmarshal(trimmed, &req); err != nil {
		return Request{}, &Error{Code: CodeParseError, Message: "request is not valid JSON"}
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return Request{}, &Error{Code: CodeInvalidRequest, Message: "request must be a JSON-RPC 2.0 call with a method"}
	}
	return req, nil
}

func NewResult(id json.RawMessage, result any) Response {
	return Response{JSONRPC: "2.0", ID: nullIfEmpty(id), Result: result}
}

func NewError(id json.RawMessage, code int, message string) Response {
	return Response{JSONRPC: "2.0", ID: nullIfEmpty(id), Error: &Error{Code: code, Message: message}}
}

// NewErrorData adds the data member, which the version error uses to list the
// versions the server does support.
func NewErrorData(id json.RawMessage, code int, message string, data any) Response {
	return Response{JSONRPC: "2.0", ID: nullIfEmpty(id), Error: &Error{Code: code, Message: message, Data: data}}
}

// ModernRequestVersion returns the version a request declares in its own _meta, which
// revision 2026-07-28 requires on every request. The second result is false for a
// request without one, the shape a 2025-06-18 client sends.
func ModernRequestVersion(params json.RawMessage) (string, bool) {
	var envelope struct {
		Meta struct {
			Version string `json:"io.modelcontextprotocol/protocolVersion"`
		} `json:"_meta"`
	}
	if len(params) == 0 || json.Unmarshal(params, &envelope) != nil || envelope.Meta.Version == "" {
		return "", false
	}
	return envelope.Meta.Version, true
}

const (
	base64HeaderPrefix = "=?base64?"
	base64HeaderSuffix = "?="
)

// HeaderValue reads a header that may carry its value in the Base64 sentinel form,
// which clients use for a name outside plain ASCII. The second result is false when
// the sentinel wraps something that is not Base64.
func HeaderValue(header string) (string, bool) {
	if len(header) < len(base64HeaderPrefix)+len(base64HeaderSuffix) ||
		!strings.HasPrefix(header, base64HeaderPrefix) || !strings.HasSuffix(header, base64HeaderSuffix) {
		return header, true
	}
	raw, err := base64.StdEncoding.DecodeString(header[len(base64HeaderPrefix) : len(header)-len(base64HeaderSuffix)])
	if err != nil {
		return "", false
	}
	return string(raw), true
}

// CompleteResult returns a result object with resultType set to complete, which
// revision 2026-07-28 puts on every result. The fields are read back as raw JSON, so
// the numbers and the order of the others stay as they were.
func CompleteResult(result any) (map[string]json.RawMessage, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	fields["resultType"] = json.RawMessage(`"complete"`)
	return fields, nil
}

// nullIfEmpty keeps the id as JSON null when the request had none, as the spec asks
// for parse and invalid-request errors.
func nullIfEmpty(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

// ToolResult is the result of tools/call. A failed tool travels here with IsError
// set, so the model can read the reason and correct the call. Protocol failures
// stay JSON-RPC errors.
type ToolResult struct {
	Content []TextContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func TextResult(text string, isError bool) ToolResult {
	return ToolResult{Content: []TextContent{{Type: "text", Text: text}}, IsError: isError}
}
