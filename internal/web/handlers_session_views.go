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

// handleMiniBar renders the phone "now tracking" bar shown above the tab
// bar on every page but the dashboard. Empty body when nothing runs.
func (s *Server) handleMiniBar(w http.ResponseWriter, r *http.Request) {
	vm, err := s.buildMiniBarVM(r)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.renderFragment(w, "minibar", vm)
}
