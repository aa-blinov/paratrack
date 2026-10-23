package web

import (
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// reconcileView is the reconciliation card on the invoices page (FIN-04): one row per
// client for the window the manager asked for, with the time in each column.
type reconcileView struct {
	From string
	To   string
	Rows []reconcileRow
	Note string
}

type reconcileRow struct {
	Client     string
	Worked     string
	Billed     string
	NotBilled  string
	Difference string
	Mismatched bool
}

// reconcileOf answers the reconciliation for the window in the query, both days included
// in the viewer's zone. Without a window the card asks for one.
func (s *Server) reconcileOf(r *http.Request, now time.Time) (*reconcileView, error) {
	loc := userLoc(r)
	from, errFrom := time.ParseInLocation("2006-01-02", r.URL.Query().Get("reconcile_from"), loc)
	to, errTo := time.ParseInLocation("2006-01-02", r.URL.Query().Get("reconcile_to"), loc)
	if errFrom != nil || errTo != nil {
		return nil, nil
	}
	view := &reconcileView{From: from.Format("2006-01-02"), To: to.Format("2006-01-02"), Rows: []reconcileRow{}}
	if to.Before(from) {
		view.Note = i18n.T(resolveLang(r), "fin.reconcileBadPeriod")
		return view, nil
	}
	rows, err := s.services.Invoicing.Queries.Reconcile(r.Context(), appmodel.ReconcileQuery{
		TeamID: teamID(r), From: from, Until: to.AddDate(0, 0, 1), Now: now,
	})
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		view.Rows = append(view.Rows, reconcileRow{
			Client:     row.ClientName,
			Worked:     fmtDur(r, row.WorkedSeconds),
			Billed:     fmtDur(r, row.BilledSeconds),
			NotBilled:  fmtDur(r, row.NotBilledSeconds),
			Difference: fmtDur(r, row.DiffSeconds),
			Mismatched: row.DiffSeconds != 0,
		})
	}
	return view, nil
}
