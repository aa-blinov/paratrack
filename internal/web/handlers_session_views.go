package web

import (
	"net/http"
)

// ---------- shared helpers ----------------------------------------

// respondActiveList is the standard "refresh the active sessions list"
// response sent by stop/pause/resume/focus. Keeps HTMX swap targets
// consistent across actions.
func (s *Server) respondActiveList(w http.ResponseWriter, r *http.Request) {
	vm, err := s.buildActiveListVM(r)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	s.renderFragment(w, "active-list", vm)
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
