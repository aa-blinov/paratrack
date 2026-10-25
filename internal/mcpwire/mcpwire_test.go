package mcpwire

import (
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
	if got := Negotiate(ProtocolVersion); got != ProtocolVersion {
		t.Fatalf("supported revision = %q", got)
	}
	if got := Negotiate("2024-11-05"); got != ProtocolVersion {
		t.Fatalf("unknown revision = %q, want the server's newest", got)
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
