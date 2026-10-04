package web

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLogUsesRoutePatternInsteadOfPathValues(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /invites/{token}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	var output bytes.Buffer
	handler := logRequests(mux, log.New(&output, "", 0), mux)
	secret := "invite-secret-value"
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/invites/"+secret, nil))

	logged := output.String()
	if strings.Contains(logged, secret) {
		t.Fatalf("access log exposed path token: %q", logged)
	}
	if !strings.Contains(logged, "GET /invites/{token} 204") {
		t.Fatalf("access log = %q, want matched route pattern and status", logged)
	}
}
