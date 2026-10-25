package mcpwire

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestParseRefusesBatchesAndMalformedMessages(t *testing.T) {
	if _, err := Parse([]byte(`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`)); err == nil || err.Code != CodeInvalidRequest {
		t.Fatalf("batch = %+v, want invalid request", err)
	}
	if _, err := Parse([]byte(`{"jsonrpc":`)); err == nil || err.Code != CodeParseError {
		t.Fatalf("broken JSON = %+v, want parse error", err)
	}
	if _, err := Parse([]byte(`{"jsonrpc":"1.0","id":1,"method":"ping"}`)); err == nil || err.Code != CodeInvalidRequest {
		t.Fatalf("wrong version = %+v, want invalid request", err)
	}
	if _, err := Parse([]byte(`{"jsonrpc":"2.0","id":1}`)); err == nil || err.Code != CodeInvalidRequest {
		t.Fatalf("no method = %+v, want invalid request", err)
	}
}

func TestNotificationHasNoID(t *testing.T) {
	req, err := Parse([]byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil || !req.IsNotification() {
		t.Fatalf("notification = %+v, %+v", req, err)
	}
	req, err = Parse([]byte(`{"jsonrpc":"2.0","id":"abc","method":"ping"}`))
	if err != nil || req.IsNotification() {
		t.Fatalf("request with a string id = %+v, %+v", req, err)
	}
}

func TestNegotiateAnswersTheClientsRevisionOnlyWhenSupported(t *testing.T) {
	if got := Negotiate(LegacyVersion); got != LegacyVersion {
		t.Fatalf("supported revision = %q", got)
	}
	if got := Negotiate("2024-11-05"); got != LegacyVersion {
		t.Fatalf("unknown revision = %q, want the server's newest", got)
	}
}

func TestModernRequestVersionReadsOnlyTheRequestsOwnMeta(t *testing.T) {
	version, ok := ModernRequestVersion(json.RawMessage(`{"name":"x","_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`))
	if !ok || version != ModernVersion {
		t.Fatalf("modern request = %q, %v", version, ok)
	}
	for _, params := range []string{``, `{"name":"x"}`, `{"_meta":{"progressToken":1}}`, `{"_meta":"2026-07-28"}`} {
		if version, ok := ModernRequestVersion(json.RawMessage(params)); ok {
			t.Fatalf("params %s read as modern %q", params, version)
		}
	}
}

func TestHeaderValueDecodesTheBase64Sentinel(t *testing.T) {
	if got, ok := HeaderValue("projects_list"); !ok || got != "projects_list" {
		t.Fatalf("plain header = %q, %v", got, ok)
	}
	encoded := "=?base64?" + base64.StdEncoding.EncodeToString([]byte("Проекты")) + "?="
	if got, ok := HeaderValue(encoded); !ok || got != "Проекты" {
		t.Fatalf("encoded header = %q, %v", got, ok)
	}
	if _, ok := HeaderValue("=?base64?not base64!?="); ok {
		t.Fatal("a sentinel around a value that is not Base64 was accepted")
	}
	if got, ok := HeaderValue("=?base64?="); !ok || got != "=?base64?=" {
		t.Fatalf("a short sentinel is a plain value, got %q, %v", got, ok)
	}
}

func TestCompleteResultAddsResultTypeBesideTheFields(t *testing.T) {
	fields, err := CompleteResult(TextResult("нельзя", true))
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"content":[{"type":"text","text":"нельзя"}],"isError":true,"resultType":"complete"}` {
		t.Fatalf("completed = %s", body)
	}
}

// A protocol error for a message we could not read carries a null id, as the spec asks.
func TestErrorWithoutIDEncodesNull(t *testing.T) {
	body, err := json.Marshal(NewError(nil, CodeParseError, "bad"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"bad"}}` {
		t.Fatalf("encoded = %s", body)
	}
}

func TestToolFailureStaysAResult(t *testing.T) {
	body, err := json.Marshal(NewResult(json.RawMessage(`7`), TextResult("нельзя", true)))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"jsonrpc":"2.0","id":7,"result":{"content":[{"type":"text","text":"нельзя"}],"isError":true}}` {
		t.Fatalf("encoded = %s", body)
	}
}
