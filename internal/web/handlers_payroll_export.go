package web

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

// payrollRunCSVHeader is the column order of a pay run's CSV: one row per person,
// with the numbers the run page shows, for the transfer into the accountant's books.
var payrollRunCSVHeader = []string{"run", "person", "hours", "seconds", "rate", "amount", "currency"}

// payrollDayCSVHeader is the breakdown behind a run: one row per person per day with
// time, and the Monday or Sunday that opens the day's week, so a sheet can sum by week.
var payrollDayCSVHeader = []string{"run", "person", "week_start", "day", "hours", "seconds"}

// handlePayrollExport writes a pay run as CSV. By default one row per
// person; ?by=day gives the day breakdown. The amounts stay on the person rows: a
// week's amount rounded on its own would not add up to the person's total.
func (s *Server) handlePayrollExport(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
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
	run := details.Run
	loc := userLoc(r)
	s.audit(r, "payroll.export", run.Number, "")
	if r.URL.Query().Get("by") == "day" {
		s.writePayrollDayCSV(w, r, details, loc)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+run.Number+`.csv"`)
	if err := writePayrollRunCSV(w, run.Number, run.Currency, details.Lines, dialectOf(r)); err != nil {
		s.logInternalError(err)
	}
}

func (s *Server) writePayrollDayCSV(w http.ResponseWriter, r *http.Request, details appmodel.PayrollRunDetail, loc *time.Location) {
	run := details.Run
	days := runDays(run.PeriodStart.In(loc), run.PeriodEnd.In(loc))
	if days == nil {
		http.Error(w, "pay run is too long for a day breakdown", http.StatusBadRequest)
		return
	}
	tracked, err := s.services.Scheduling.TrackedSecondsByPersonDay(r.Context(), appmodel.PersonDayQuery{TeamID: teamID(r), Days: days, Now: userNow(r)})
	if err != nil {
		s.writeInternalError(w, err)
		return
	}
	sunday := weekStartsSunday(r)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+run.Number+`-days.csv"`)
	out, err := dialectOf(r).writer(w)
	if err != nil {
		s.logInternalError(err)
		return
	}
	if err := out.Write(payrollDayCSVHeader); err != nil {
		s.logInternalError(err)
		return
	}
	for _, line := range details.Lines {
		seconds := tracked[line.UserID]
		for i, day := range days {
			if i >= len(seconds) || seconds[i] <= 0 {
				continue
			}
			if err := out.Write([]string{
				run.Number, line.Label, weekStartOn(sunday, day).Format("2006-01-02"), day.Format("2006-01-02"),
				dialectOf(r).number(formatHoursDecimal(seconds[i])), strconv.Itoa(seconds[i]),
			}); err != nil {
				s.logInternalError(err)
				return
			}
		}
	}
	out.Flush()
	if err := out.Error(); err != nil {
		s.logInternalError(err)
	}
}

// writePayrollRunCSV writes the person rows of a run. Hours use a decimal point
// in any language, as the other exports do, and the amounts are in the run's currency.
func writePayrollRunCSV(w io.Writer, number, currency string, lines []model.PayrollLine, d csvDialect) error {
	out, err := d.writer(w)
	if err != nil {
		return err
	}
	if err := out.Write(payrollRunCSVHeader); err != nil {
		return err
	}
	for _, line := range lines {
		if err := out.Write([]string{
			number, line.Label, d.number(formatHoursDecimal(line.Seconds)), strconv.Itoa(line.Seconds),
			d.number(money.FormatCents(line.RateCents)), d.number(money.FormatCents(line.AmountCents)), currency,
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}
