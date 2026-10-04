package web

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

type unencodableJSONResponse struct {
	Unsupported chan int `json:"unsupported"`
}

func (unencodableJSONResponse) isJSONResponse() {}

func TestInternalErrorHidesDetailsFromHTTPResponse(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{logger: log.New(io.Discard, "", 0)}).writeInternalError(w, errors.New("database password leaked"))

	if w.Code != 500 {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "database password leaked") {
		t.Fatalf("response exposed internal error: %q", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "internal server error") {
		t.Fatalf("unexpected response body: %q", w.Body.String())
	}
}

func TestInternalJSONErrorHidesDetailsFromHTTPResponse(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{logger: log.New(io.Discard, "", 0)}).writeInternalJSONError(w, errors.New("database password leaked"))

	if w.Code != 500 {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if strings.Contains(w.Body.String(), "database password leaked") {
		t.Fatalf("response exposed internal error: %q", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"error":"internal server error"`) {
		t.Fatalf("unexpected response body: %q", w.Body.String())
	}
}

func TestJSONEncodingFailureReturnsStableInternalErrorBeforeStatus(t *testing.T) {
	w := httptest.NewRecorder()
	var logs bytes.Buffer
	server := &Server{logger: log.New(&logs, "", 0)}
	server.writeJSONStatus(w, 400, unencodableJSONResponse{Unsupported: make(chan int)})

	if w.Code != 500 {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if got := w.Body.String(); got != "{\"error\":\"internal server error\"}\n" {
		t.Fatalf("body = %q, want stable JSON error", got)
	}
	if !strings.Contains(logs.String(), "encode JSON response") || !strings.Contains(logs.String(), "chan int") {
		t.Fatalf("logs = %q, want encoding cause", logs.String())
	}
}
