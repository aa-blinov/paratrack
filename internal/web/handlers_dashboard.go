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
	s.render(w, r, "dashboard-content", &data)
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
	data.setManage(canManage(r))
	data.setModules(s.userModules(r))
	data.setWidgets(prefsOf(r).HiddenWidgets)
	data.setCSRF(ensureCSRF(w, r))
	w.Header().Set("Cache-Control", "no-store")
	s.writeJSON(w, dashboardPageResponse{Data: data})
}
