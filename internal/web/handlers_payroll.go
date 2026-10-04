package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

func (s *Server) payrollErrorMessage(r *http.Request, err error) string {
	switch {
	case errors.Is(err, appmodel.ErrNoPayableTime):
		return "no paid members with tracked time in that period"
	case errors.Is(err, appmodel.ErrInvalidPayrollPeriod):
		return "payroll period must end after it starts"
	case errors.Is(err, appmodel.ErrInvalidPayrollTeam), errors.Is(err, model.ErrNotFound):
		return "pay run not found"
	case errors.Is(err, appmodel.ErrForbidden), errors.Is(err, model.ErrForbidden):
		return "manager role required"
	default:
		s.logInternalError(err)
		return i18n.T(resolveLang(r), "err.internal")
	}
}

// handlePayroll lists runs and offers a generator.
func (s *Server) handlePayroll(w http.ResponseWriter, r *http.Request) {
	list, err := s.services.Payroll.ListRuns(r.Context(), appmodel.PayrollRunListQuery{TeamID: teamID(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	lang := string(resolveLang(r))
	data := payrollPage{pageData: pageData{Title: "Payroll", Active: "payroll", Lang: lang}}
	for _, item := range list {
		run := item.Run
		data.Items = append(data.Items, payrollSummary{
			ID: run.ID, Number: run.Number, Status: run.Status,
			Total: moneyL(resolveLang(r), item.TotalCents, run.Currency), Hours: fmtHoursL(resolveLang(r), item.TotalHoursHundredths),
			Period: fmtDay(resolveLang(r), run.PeriodStart) + " – " + fmtDay(resolveLang(r), run.PeriodEnd.AddDate(0, 0, -1)),
		})
	}
	now := userNow(r)
	data.DefStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	data.DefEnd = now.Format("2006-01-02")
	// Coming back from a create that overlapped earlier runs: ask first.
	if q := r.URL.Query(); q.Get("overlap") != "" {
		data.Overlap = q.Get("overlap")
		data.DefStart, data.DefEnd, data.DefNotes = q.Get("start"), q.Get("end"), q.Get("notes")
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, "Payroll", "payroll", "payroll", &data)
}

type payrollSummary struct {
	ID     int64
	Number string
	Status string
	Total  string
	Hours  string
	Period string
}

type payrollPage struct {
	pageData
	Items    []payrollSummary
	DefStart string
	DefEnd   string
	DefNotes string
	Overlap  string // runs whose period this one overlaps, "PAY-…, PAY-…"
	Flash    string
	FlashOK  bool
}

func (p *payrollPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handlePayrollCreate generates a pay run from tracked time.
func (s *Server) handlePayrollCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	startStr := strings.TrimSpace(r.PostForm.Get("start"))
	endStr := strings.TrimSpace(r.PostForm.Get("end"))
	notes := strings.TrimSpace(r.PostForm.Get("notes"))
	start, err1 := time.ParseInLocation("2006-01-02", startStr, userLoc(r))
	end, err2 := time.ParseInLocation("2006-01-02", endStr, userLoc(r))
	if err1 != nil || err2 != nil || end.Before(start) {
		http.Redirect(w, r, "/payroll?flash="+encodeFlash(false, "bad period"), http.StatusSeeOther)
		return
	}
	end = end.AddDate(0, 0, 1)
	// The overlap warning and draft creation share the store transaction, so
	// concurrent requests cannot both pass a stale preflight check.
	run, overlaps, err := s.services.Payroll.CreateRun(operationContext(r), appmodel.PayrollDraftRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Notes: notes,
		Start: start, End: end, ConfirmOverlap: r.PostForm.Get("confirm") != "",
	})
	if err == nil && len(overlaps) > 0 {
		clash := make([]string, 0, len(overlaps))
		for _, overlap := range overlaps {
			clash = append(clash, overlap.Number)
		}
		q := url.Values{"overlap": {strings.Join(clash, ", ")}, "start": {startStr}, "end": {endStr}, "notes": {notes}}
		http.Redirect(w, r, "/payroll?"+q.Encode(), http.StatusSeeOther)
		return
	}
	if err != nil {
		msg := s.payrollErrorMessage(r, err)
		http.Redirect(w, r, "/payroll?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/payroll/"+strconv.FormatInt(run.ID, 10), http.StatusSeeOther)
}

// handlePayrollDetail renders one run.
func (s *Server) handlePayrollDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/payroll/"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	details, err := s.services.Payroll.GetRun(r.Context(), teamID(r), id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			s.writeInternalError(w, err)
		}
		return
	}
	run, lines := details.Run, details.Lines
	lang := string(resolveLang(r))
	vms := make([]payrollLineVM, 0, len(lines))
	for _, l := range lines {
		vms = append(vms, payrollLineVM{
			Label: l.Label, Hours: fmtHoursL(resolveLang(r), money.HoursHundredths(l.Seconds)),
			Rate: moneyL(resolveLang(r), l.RateCents, run.Currency), Amount: moneyL(resolveLang(r), l.AmountCents, run.Currency),
		})
	}
	data := payrollDetailPage{
		pageData: pageData{Title: run.Number, Active: "payroll", Lang: lang},
		Run: payrollVM{
			ID: run.ID, Number: run.Number, Status: run.Status, Notes: run.Notes,
			PeriodLabel: fmtDate(resolveLang(r), run.PeriodStart) + " – " + fmtDate(resolveLang(r), run.PeriodEnd.AddDate(0, 0, -1)),
			Lines:       vms, Total: moneyL(resolveLang(r), details.TotalCents, run.Currency), TotalCents: details.TotalCents,
			Hours: fmtHoursL(resolveLang(r), details.TotalHoursHundredths),
		},
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, run.Number, "payroll", "payroll-detail", &data)
}

type payrollLineVM struct {
	Label  string
	Hours  string
	Rate   string
	Amount string
}

type payrollVM struct {
	ID          int64
	Number      string
	Status      string
	Notes       string
	PeriodLabel string
	Lines       []payrollLineVM
	Total       string
	TotalCents  int
	Hours       string
}

type payrollDetailPage struct {
	pageData
	Run     payrollVM
	Flash   string
	FlashOK bool
}

func (p *payrollDetailPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handlePayrollPaid marks a run paid.
func (s *Server) handlePayrollPaid(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx := operationContext(r)
	if err := s.services.PayrollPaid.MarkPaid(ctx, appmodel.PayrollMutationRequest{TeamID: teamID(r), RunID: id, CallerID: authenticatedUserID(r)}); err != nil {
		msg := s.payrollErrorMessage(r, err)
		http.Redirect(w, r, "/payroll?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(true, "marked paid"),
		http.StatusSeeOther)
}

// handlePayrollDelete removes a draft run.
func (s *Server) handlePayrollDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.services.Payroll.DeleteDraft(r.Context(), appmodel.PayrollMutationRequest{TeamID: teamID(r), RunID: id, CallerID: authenticatedUserID(r)}); err != nil {
		msg := s.payrollErrorMessage(r, err)
		http.Redirect(w, r, "/payroll?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/payroll?flash=removed", http.StatusSeeOther)
}

// handleMemberPay saves a member's pay rate + daily capacity.
func (s *Server) handleMemberPay(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, "invalid form"), http.StatusSeeOther)
		return
	}
	uid, err := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, "bad user"), http.StatusSeeOther)
		return
	}
	caller, ok := UserFrom(r.Context()) // the manage middleware authenticates the manager
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var pay, cap *int
	if n, has, err := formCents(r, "hourly_pay"); err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, i18n.T(resolveLang(r), "bill.badRate")), http.StatusSeeOther)
		return
	} else if has {
		pay = &n
	}
	if v := strings.TrimSpace(r.PostForm.Get("capacity_minutes")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, "bad capacity"), http.StatusSeeOther)
			return
		}
		cap = &n
	}
	if err := s.services.Payroll.UpdateMemberPay(operationContext(r), appmodel.PayrollMemberPayRequest{TeamID: teamID(r), UserID: uid, CallerID: caller.ID, PayCents: pay, CapacityMinutes: cap}); err != nil {
		if errors.Is(err, appmodel.ErrForbidden) || errors.Is(err, appmodel.ErrNotFound) {
			http.Redirect(w, r, "/settings/members?flash=forbidden", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, s.payrollErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings/members?flash="+encodeFlash(true, "updated"), http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
