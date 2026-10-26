package web

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

// Deleting a workspace with other members is refused in the language of the person who
// asked, not with the English text the service wraps.
func TestWorkspaceWithMembersRefusalIsReadable(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("GET", "/settings/agent-actions", nil)
	got := s.teamErrorMessage(r, errors.New("validation: remove all members first"))
	if got == "" || strings.Contains(got, "remove all members") || got == "err.invalidInput" {
		t.Fatalf("refusal text = %q, want the translated sentence", got)
	}
}
