package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// statusWriter keeps the status and the cause of a 5xx, and nothing of a 2xx.
func TestStatusWriterKeepsFailureCause(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	http.Error(sw, "db: connection refused", 500)
	if sw.status != 500 || !strings.Contains(string(sw.body), "connection refused") {
		t.Fatalf("status %d body %q", sw.status, sw.body)
	}
	ok := &statusWriter{ResponseWriter: httptest.NewRecorder()}
	ok.Write([]byte(strings.Repeat("x", 1000)))
	if ok.status != 200 || len(ok.body) != 0 {
		t.Fatalf("2xx must not be kept: status %d, %d bytes", ok.status, len(ok.body))
	}
}
