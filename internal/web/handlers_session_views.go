package web

import (
	"net/http"
)

// respondActiveList answers a timer action. The React dashboard reloads its own
// state after every action, so the answer carries no body: the toast and undo
// headers were set by the action itself.
func (s *Server) respondActiveList(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
