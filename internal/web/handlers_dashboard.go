package web

import (
	"net/http"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	now := userNow(r)
	snapshot, err := s.services.Dashboard.Build(r.Context(), appmodel.DashboardQuery{
		TeamID: teamID(r), Now: now,
		IncludeBilling: canManage(r) && s.userModules(r)["invoices"],
	})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data, err := buildDashboardData(r, now, snapshot)
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	data.Mode = presetKeyForModules(s.teamModules(r))
	data.OpenTasks = s.openTaskOptions(r)
	data.SectionsOpen, data.SectionsTotal = sectionsOpenFor(s.userModules(r), canManage(r))
	data.SectionsPersonal = !canManage(r)
	s.render(w, r, &data)
}

func (s *Server) handleAPIDashboard(w http.ResponseWriter, r *http.Request) {
	now := userNow(r)
	snapshot, err := s.services.Dashboard.Build(r.Context(), appmodel.DashboardQuery{
		TeamID: teamID(r), Now: now,
		IncludeBilling: canManage(r) && s.userModules(r)["invoices"],
	})
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	data, err := buildDashboardData(r, now, snapshot)
	if err != nil {
		s.writeInternalJSONError(w, err)
		return
	}
	data.Mode = presetKeyForModules(s.teamModules(r))
	data.OpenTasks = s.openTaskOptions(r)
	// Same as the page: without the counts the mode line lost «открыто N из M»
	// on every refresh, and the dashboard refreshes each minute a timer runs.
	data.SectionsOpen, data.SectionsTotal = sectionsOpenFor(s.userModules(r), canManage(r))
	data.SectionsPersonal = !canManage(r)
	data.setManage(canManage(r))
	data.setModules(s.userModules(r))
	data.setWidgets(prefsOf(r).HiddenWidgets)
	data.setCSRF(ensureCSRF(w, r))
	w.Header().Set("Cache-Control", "no-store")
	s.writeJSON(w, dashboardPageResponse{Data: data})
}
