package web

import (
	"html/template"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type brokenFragmentTemplateData struct{}

func (brokenFragmentTemplateData) isTemplateData()         {}
func (brokenFragmentTemplateData) isFragmentTemplateData() {}

func TestRenderFragmentDoesNotCommitPartialTemplateOutput(t *testing.T) {
	s := &Server{tmpl: template.Must(template.New("broken").Parse("partial{{.Missing}}")), logger: log.New(io.Discard, "", 0)}
	w := httptest.NewRecorder()
	s.renderFragment(w, "broken", brokenFragmentTemplateData{})

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if strings.Contains(w.Body.String(), "partial") {
		t.Fatalf("response contains partial template output: %q", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "internal server error") {
		t.Fatalf("response does not contain generic error: %q", w.Body.String())
	}
}
