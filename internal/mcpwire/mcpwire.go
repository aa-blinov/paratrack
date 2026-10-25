// Package mcpwire holds the JSON-RPC 2.0 messages of the Model Context Protocol
// that paratrack speaks over HTTP. It knows nothing about the product: tools and
// their rules live in the transport adapter.
package mcpwire

import (
	"bytes"
	"encoding/json"
)

// ProtocolVersion is the MCP revision the server answers with. Batching was removed
// in this revision, so one request is one JSON object.
const ProtocolVersion = "2025-06-18"

// Negotiate returns the revision to answer with: the client's own when the server
// speaks it, otherwise the newest revision the server speaks.
func Negotiate(requested string) string {
	if requested == ProtocolVersion {
		return requested
	}
	return ProtocolVersion
}

const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
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
}

// Parse decodes one request. A malformed message or a batch is a protocol error,
// and the caller answers it with the returned Error and a null id.
func Parse(body []byte) (Request, *Error) {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) == 0 {
		return Request{}, &Error{Code: CodeInvalidRequest, Message: "empty request body"}
	}
	if trimmed[0] == '[' {
		return Request{}, &Error{Code: CodeInvalidRequest, Message: "batches are not supported in protocol 2025-06-18"}
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
