package web

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusRecorderTracksFinalResponseStatus(t *testing.T) {
	underlying := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: underlying, status: http.StatusOK}
	recorder.WriteHeader(http.StatusCreated)
	recorder.WriteHeader(http.StatusInternalServerError)

	if recorder.status != http.StatusCreated {
		t.Fatalf("recorded status = %d, want %d", recorder.status, http.StatusCreated)
	}
	if got := recorder.Unwrap(); got != underlying {
		t.Fatalf("Unwrap() = %T, want underlying ResponseWriter", got)
	}
}

func TestStatusRecorderWriteCommitsDefaultOKStatus(t *testing.T) {
	underlying := httptest.NewRecorder()
	recorder := &statusRecorder{ResponseWriter: underlying, status: http.StatusOK}
	if _, err := recorder.Write([]byte("ok")); err != nil {
		t.Fatalf("Write(): %v", err)
	}
	if recorder.status != http.StatusOK || !recorder.wroteHeader {
		t.Fatalf("status/wroteHeader = %d/%v, want %d/true", recorder.status, recorder.wroteHeader, http.StatusOK)
	}
}

func TestLogRequestsLogsAndRepanicsHandlerPanic(t *testing.T) {
	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	routes := http.NewServeMux()
	routes.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) {
		panic("handler failed")
	})
	handler := logRequests(routes, logger, routes)
	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	func() {
		defer func() {
			if recovered := recover(); recovered != "handler failed" {
				t.Fatalf("recovered panic = %v, want original panic", recovered)
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}()
	if !strings.Contains(output.String(), "GET GET /panic 500 ") {
		t.Fatalf("panic request log = %q, want route and status 500", output.String())
	}
}
