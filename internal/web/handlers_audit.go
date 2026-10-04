package web

import (
	"net/http"

	"github.com/aa-blinov/paratrack/internal/appmodel"
)

func (s *Server) handleAuditPage(w http.ResponseWriter, r *http.Request) {
	list, err := s.services.AuditLog.List(r.Context(), appmodel.AuditListQuery{TeamID: teamID(r), Limit: 100})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	lang := string(resolveLang(r))
	data := auditPage{pageData: pageData{Title: "Audit log", Active: "settings-audit", Lang: lang}}
	for _, e := range list {
		data.Items = append(data.Items, auditRow{
			Time: e.CreatedAt.Format("2006-01-02 15:04"), Action: e.Action,
			Target: e.Target, IP: e.IP,
		})
	}
	s.renderPageForRequest(w, r, "Audit log", "settings-audit", "audit", &data)
}

type auditRow struct {
	Time   string
	Action string
	Target string
	IP     string
}

type auditPage struct {
	pageData
	Items   []auditRow
	Flash   string
	FlashOK bool
}

func (p *auditPage) setCSRF(t string) { p.pageData.setCSRF(t) }
