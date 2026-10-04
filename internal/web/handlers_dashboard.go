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
