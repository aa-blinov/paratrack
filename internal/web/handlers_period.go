package web

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Closing a month as a whole (FIN-25). The accountant closes one month from
// the payouts page; a closed month refuses edits to its hours and documents
// that would touch it, and the reopen names who can lift the gate.

// handlePeriodClose fixes one month: its hours, invoices and payouts.
func (s *Server) handlePeriodClose(w http.ResponseWriter, r *http.Request) {
	s.changePeriodClose(w, r, true)
}

// handlePeriodReopen lifts the month's close again.
func (s *Server) handlePeriodReopen(w http.ResponseWriter, r *http.Request) {
	s.changePeriodClose(w, r, false)
}

func (s *Server) changePeriodClose(w http.ResponseWriter, r *http.Request, close bool) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	lang := resolveLang(r)
	// The form sends the month's first day ("2006-01-02"). The month is a
	// calendar name and travels as a string: re-deriving it from a midnight
	// instant would drag it into the month before in any zone behind the
	// viewer's, so the store normalises to the string.
	month := r.PostForm.Get("month")
	request := appmodel.PeriodCloseRequest{TeamID: teamID(r), CallerID: authenticatedUserID(r), Month: month}
	change, auditAction, okKey := s.services.Tracking.Commands.ReopenPeriod, "period.reopen", "pay.periodReopenDone"
	if close {
		change, auditAction, okKey = s.services.Tracking.Commands.ClosePeriod, "period.close", "pay.periodClosedDone"
	}
	if err := change(r.Context(), request); err != nil {
		s.periodFlash(w, r, false, periodCloseMessage(lang, err))
		return
	}
	s.audit(r, auditAction, month, "")
	s.periodFlash(w, r, true, i18n.T(lang, okKey))
}

// periodFlash sends the reader back to the payouts page's closed-months block.
func (s *Server) periodFlash(w http.ResponseWriter, r *http.Request, ok bool, msg string) {
	http.Redirect(w, r, "/payroll?flash="+encodeFlash(ok, msg)+"#period", http.StatusSeeOther)
}

// periodCloseMessage turns a closing refusal into the sentence the accountant
// reads, naming the month the way the page does.
func periodCloseMessage(lang i18n.Lang, err error) string {
	switch {
	case errors.Is(err, model.ErrMonthNotFinished):
		return i18n.T(lang, "pay.periodNotOver")
	case errors.Is(err, model.ErrWeekHasRunningTimer):
		return i18n.T(lang, "pay.periodRunning")
	case errors.Is(err, model.ErrMonthHasDrafts):
		return i18n.T(lang, "pay.periodDrafts")
	case errors.Is(err, model.ErrForbidden):
		return i18n.T(lang, "err.forbidden")
	default:
		return i18n.T(lang, "err.generic")
	}
}

// monthClosedMessage names the closed month that refused a write, the way
// weekLockedMessage names an approved week.
func monthClosedMessage(lang i18n.Lang, closed *model.PeriodClosedError) string {
	return fmt.Sprintf(i18n.T(lang, "pay.periodLocked"), monthLabel(lang, closed.Month))
}

// monthLabel renders the month's first day as the page reads it: the month's
// name and the year, in the reader's language.
func monthLabel(lang i18n.Lang, at time.Time) string {
	return i18n.T(lang, "period.month"+at.Format("01")) + " " + at.Format("2006")
}

// closedMonthsFor reads the closed months for the payouts page and resolves
// who closed each one, newest first.
func (s *Server) closedMonthsFor(r *http.Request) []payrollClosedMonth {
	items, err := s.services.Tracking.Queries.ClosedMonths(r.Context(), teamID(r))
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("web: closed months: %v", err)
		}
		return nil
	}
	if len(items) == 0 {
		return nil
	}
	lang := resolveLang(r)
	byID := make(map[int64]string, len(items))
	for _, person := range s.taskPeople(r) {
		byID[person.ID] = person.Name
	}
	out := make([]payrollClosedMonth, 0, len(items))
	for _, item := range items {
		out = append(out, payrollClosedMonth{
			Month:     item.Month.Format("2006-01-02"),
			Label:     monthLabel(lang, item.Month),
			ClosedOn:  item.ClosedOn.In(userLoc(r)).Format("02.01.2006"),
			ClosedBy:  byID[item.ClosedByID],
			CanReopen: mayReopenMonth(RoleFrom(r.Context()), authenticatedUserID(r), item),
		})
	}
	return out
}

// payrollClosedMonth is one closed month on the payouts page: the accountant
// reads what is fixed and who fixed it.
type payrollClosedMonth struct {
	Month    string // the month's first day, YYYY-MM-DD, the reopen form sends it back
	Label    string
	ClosedOn string
	ClosedBy string
	// CanReopen follows the store's rule, so the screen offers reopening only
	// where the store would allow it.
	CanReopen bool
}

// mayReopenMonth mirrors ReopenMonth: the owner reopens any month, whoever
// closed one reopens it, and an admin the rest but a month an accountant closed.
func mayReopenMonth(role model.TeamRole, callerID int64, item appmodel.PeriodClose) bool {
	switch {
	case role.IsOwner(), item.ClosedByID == callerID && role.CanReadFinance():
		return true
	case role == model.TeamRoleAdmin:
		return item.ClosedByRole != string(model.TeamRoleAccountant)
	}
	return false
}
