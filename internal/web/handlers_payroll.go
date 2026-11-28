package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

func (s *Server) payrollErrorMessage(r *http.Request, err error) string {
	var notApproved *model.PayrollNotApprovedError
	if errors.As(err, &notApproved) {
		loc := userLoc(r)
		return fmt.Sprintf(i18n.T(resolveLang(r), "pay.notApproved"), notApproved.Count, notApproved.First.In(loc).Format("02.01.2006"), notApproved.Last.In(loc).Format("02.01.2006"))
	}
	switch {
	case errors.Is(err, model.ErrPayrollNotDraft):
		return i18n.T(resolveLang(r), "pay.recalcDraftOnly")
	case errors.Is(err, model.ErrAdjustmentNoBase):
		return i18n.T(resolveLang(r), "pay.adjNoBase")
	case errors.Is(err, appmodel.ErrNoPayRates):
		return "no member has an hourly rate"
	case errors.Is(err, appmodel.ErrNoPayableTime):
		return "no paid members with tracked time in that period"
	case errors.Is(err, appmodel.ErrInvalidPayrollPeriod):
		return "payroll period must end after it starts"
	case errors.Is(err, appmodel.ErrInvalidPayrollTeam), errors.Is(err, model.ErrNotFound):
		return "pay run not found"
	case errors.Is(err, appmodel.ErrForbidden), errors.Is(err, model.ErrForbidden):
		// The draft-only refusal of deleting or editing a run; the caller is
		// the manager, so the flash says that and not "you lack the role".
		// A recalculation has its own ErrPayrollNotDraft above.
		return "pay run is draft only"
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
	data := payrollPage{
		pageData:     pageData{Title: "Payroll", Active: "payroll", Lang: lang, ReactApp: true},
		PayrollReact: true,
		Items:        make([]payrollSummary, 0, len(list)),
	}
	for _, item := range list {
		run := item.Run
		data.Items = append(data.Items, payrollSummary{
			ID: run.ID, Number: run.Number, Status: run.Status,
			Total: moneyL(resolveLang(r), item.TotalCents, run.Currency), Hours: fmtHoursL(resolveLang(r), item.TotalHoursHundredths),
			Period:  periodSpan(resolveLang(r), fmtDay(resolveLang(r), run.PeriodStart.In(userLoc(r))), fmtDay(resolveLang(r), run.PeriodEnd.In(userLoc(r)).AddDate(0, 0, -1))),
			Members: strings.Join(item.Members, ", "),
		})
	}
	markPeriodDuplicates(data.Items)
	now := userNow(r)
	data.DefStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	// The current week cannot be approved until it ends, so a run over it is
	// refused. The default stops at the last day of the finished weeks.
	// In the first days of a month there is no finished week in it yet: the
	// period then ends today, and the page shows why a run would be refused.
	lastFinished := startOfWeek(r, now).AddDate(0, 0, -1)
	data.DefEnd = now.Format("2006-01-02")
	if !lastFinished.Before(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())) {
		data.DefEnd = lastFinished.Format("2006-01-02")
	}
	// Coming back from a create that overlapped earlier runs: ask first.
	if q := r.URL.Query(); q.Get("overlap") != "" {
		data.Overlap = q.Get("overlap")
		data.DefStart, data.DefEnd, data.DefNotes = q.Get("start"), q.Get("end"), q.Get("notes")
		data.DefUserIDs = parseIDsParam(q.Get("user_ids"))
	}
	if q := r.URL.Query(); q.Get("overlap") == "" {
		if _, err := time.Parse("2006-01-02", q.Get("start")); err == nil {
			data.DefStart = q.Get("start")
		}
		if _, err := time.Parse("2006-01-02", q.Get("end")); err == nil {
			data.DefEnd = q.Get("end")
		}
	}
	data.Members = s.payrollMemberOptions(r)
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	from, to, asked := unapprovedWindow(r, now)
	data.UnapprovedFrom, data.UnapprovedTo, data.UnapprovedAsked = from.Format("2006-01-02"), to.Format("2006-01-02"), asked
	data.UnapprovedWeeks = s.unapprovedPayrollWeeks(r, startOfWeek(r, now), from, to)
	data.ClosedMonths = s.closedMonthsFor(r)
	s.renderPageForRequest(w, r, "Payroll", "payroll", &data)
}

// maxUnapprovedWindowWeeks bounds the period a manager can ask about: each week is
// one lookup, so a year is the most the page checks at once.
const maxUnapprovedWindowWeeks = 53

// unapprovedWindow is the span of days the page checks for unapproved weeks, both
// ends inclusive. A period the manager asked for replaces the default, which is the
// last eight finished weeks. A period that does not parse, runs backwards or is
// longer than the bound falls back to the default.
func unapprovedWindow(r *http.Request, now time.Time) (time.Time, time.Time, bool) {
	thisWeek := startOfWeek(r, now)
	from, to := thisWeek.AddDate(0, 0, -7*8), thisWeek.AddDate(0, 0, -1)
	q := r.URL.Query()
	asked, errFrom := time.ParseInLocation("2006-01-02", q.Get("unapproved_from"), now.Location())
	last, errTo := time.ParseInLocation("2006-01-02", q.Get("unapproved_to"), now.Location())
	if errFrom != nil || errTo != nil || last.Before(asked) || last.Sub(asked) > maxUnapprovedWindowWeeks*7*24*time.Hour {
		return from, to, false
	}
	return asked, last, true
}

// unapprovedPayrollWeeks lists the finished weeks with tracked time that no approval
// covers, newest first, within the window. The current week is never listed: it
// cannot be approved until it ends. One read covers the whole window; the starts are
// grouped by the week they fall in, on the viewer's week start.
func (s *Server) unapprovedPayrollWeeks(r *http.Request, thisWeek, from, to time.Time) []payrollWeekHint {
	lang := resolveLang(r)
	last := startOfWeek(r, to)
	if !last.Before(thisWeek) {
		last = thisWeek.AddDate(0, 0, -7)
	}
	first := startOfWeek(r, from)
	if last.Before(first) {
		return nil
	}
	starts, err := s.services.Payroll.UnapprovedPayrollStarts(r.Context(), teamID(r), first, last.AddDate(0, 0, 7))
	if err != nil {
		if s.logger != nil {
			s.logger.Printf("web: payroll unapproved weeks: %v", err)
		}
		return nil
	}
	counts := map[time.Time]int{}
	for _, at := range starts {
		counts[startOfWeek(r, at.In(userLoc(r)))]++
	}
	var weeks []payrollWeekHint
	for start := last; !start.Before(first); start = start.AddDate(0, 0, -7) {
		count := counts[start]
		if count == 0 {
			continue
		}
		end := start.AddDate(0, 0, 7)
		weeks = append(weeks, payrollWeekHint{
			Date:  start.Format("2006-01-02"),
			Last:  end.AddDate(0, 0, -1).Format("2006-01-02"),
			Label: periodSpan(lang, fmtDay(lang, start), fmtDay(lang, end.AddDate(0, 0, -1))),
			Count: count,
		})
	}
	return weeks
}

type payrollSummary struct {
	ID     int64
	Number string
	Status string
	Total  string
	Hours  string
	Period string
	// Members names the people the run prices (UX-44): three runs of one
	// period are told apart by who is in them.
	Members string
	// DuplicateOf is the number of the run this one repeats the period of —
	// empty everywhere but on the second and later runs of one period.
	DuplicateOf string
}

type payrollMemberOption struct {
	ID      int64
	Name    string
	PayKind string
}

type payrollPage struct {
	pageData
	PayrollReact bool
	Items        []payrollSummary
	Members      []payrollMemberOption
	DefStart     string
	DefEnd       string
	DefNotes     string
	DefUserIDs   []int64
	Overlap      string // runs whose period this one overlaps, "PAY-…, PAY-…"
	Flash        string
	FlashOK      bool
	// ClosedMonths are the months the workspace closed as a whole (FIN-25),
	// newest first: what they hold is fixed until a manager reopens them.
	ClosedMonths []payrollClosedMonth
	// UnapprovedWeeks are the finished weeks with tracked time that no approval
	// covers yet, newest first: a run over them would be refused.
	UnapprovedWeeks []payrollWeekHint
	// UnapprovedFrom and UnapprovedTo are the days checked, inclusive, YYYY-MM-DD.
	UnapprovedFrom  string
	UnapprovedTo    string
	UnapprovedAsked bool
}

type payrollWeekHint struct {
	Date  string // Monday, YYYY-MM-DD: the timesheet opens on it
	Last  string // Sunday, the last day an approval of this week takes
	Label string
	Count int
}

func (p *payrollPage) setCSRF(t string) { p.pageData.setCSRF(t) }

// handlePayrollCreate generates a pay run from tracked time.
func (s *Server) handlePayrollCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, i18n.T(resolveLang(r), "err.invalidInput"), http.StatusBadRequest)
		return
	}
	startStr := strings.TrimSpace(r.PostForm.Get("start"))
	endStr := strings.TrimSpace(r.PostForm.Get("end"))
	notes := strings.TrimSpace(r.PostForm.Get("notes"))
	start, err1 := time.ParseInLocation("2006-01-02", startStr, userLoc(r))
	end, err2 := time.ParseInLocation("2006-01-02", endStr, userLoc(r))
	if err1 != nil || err2 != nil || end.Before(start) {
		// The dates typed come back in the form, so one of them is fixed instead of both retyped.
		back := url.Values{"start": {startStr}, "end": {endStr}, "flash": {flashCode(false, "bad period")}}
		http.Redirect(w, r, "/payroll?"+back.Encode(), http.StatusSeeOther)
		return
	}
	end = end.AddDate(0, 0, 1)
	// Paying the same hours twice is allowed only with a reason, and the reason is kept (FIN-22).
	reason := strings.TrimSpace(r.PostForm.Get("overlap_reason"))
	confirm := r.PostForm.Get("confirm") != ""
	if confirm && reason == "" {
		q := url.Values{"start": {startStr}, "end": {endStr}, "notes": {notes}}
		http.Redirect(w, r, "/payroll?"+q.Encode()+"&flash="+encodeFlash(false, i18n.T(resolveLang(r), "pay.overlapNeedsReason")), http.StatusSeeOther)
		return
	}
	// The overlap warning and draft creation share the store transaction, so
	// concurrent requests cannot both pass a stale preflight check.
	userIDs := parsePostFormIDs(r.PostForm["user_id"])
	run, overlaps, err := s.services.Payroll.CreateRun(operationContext(r), appmodel.PayrollDraftRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Notes: notes,
		Start: start, End: end, ConfirmOverlap: r.PostForm.Get("confirm") != "",
		UserIDs: userIDs,
	})
	if err == nil && len(overlaps) > 0 {
		clash := make([]string, 0, len(overlaps))
		for _, overlap := range overlaps {
			clash = append(clash, overlap.Number)
		}
		q := url.Values{"overlap": {strings.Join(clash, ", ")}, "start": {startStr}, "end": {endStr}, "notes": {notes}}
		if len(userIDs) > 0 {
			q.Set("user_ids", joinIDs(userIDs))
		}
		http.Redirect(w, r, "/payroll?"+q.Encode(), http.StatusSeeOther)
		return
	}
	if err != nil {
		msg := s.payrollErrorMessage(r, err)
		http.Redirect(w, r, "/payroll?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	if confirm {
		s.audit(r, "payroll.overlap", run.Number, "reason: "+reason)
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
	loc := userLoc(r)
	days := runDays(run.PeriodStart.In(loc), run.PeriodEnd.In(loc))
	var tracked map[int64][]int
	var byProject map[int64]map[int64]int
	projectNames := map[int64]string{}
	if len(days) > 0 {
		q := appmodel.PersonDayQuery{TeamID: teamID(r), Days: days, Now: userNow(r)}
		tracked, err = s.services.Scheduling.TrackedSecondsByPersonDay(r.Context(), q)
		if err != nil {
			s.writeInternalError(w, err)
			return
		}
		byProject, err = s.services.Scheduling.TrackedSecondsByPersonProject(r.Context(), q)
		if err != nil {
			s.writeInternalError(w, err)
			return
		}
		if projList, projErr := s.services.Projects.Queries.List(r.Context(), appmodel.ProjectCatalogQuery{TeamID: teamID(r), IncludeArchived: true}); projErr == nil {
			for _, p := range projList {
				projectNames[p.ID] = p.Name
			}
		}
	}
	vms := make([]payrollLineVM, 0, len(lines))
	for i, l := range lines {
		isAdj := l.Kind == model.PayrollLineAdjustment
		// An adjustment sits indented under its person's line; one whose
		// person has no hours or salary line stands on its own, or it would
		// read as belonging to whoever came before.
		under := isAdj && i > 0 && lines[i-1].UserID == l.UserID
		hours := fmtHoursL(resolveLang(r), money.HoursHundredths(l.Seconds))
		rate := moneyL(resolveLang(r), l.RateCents, run.Currency)
		var lineDays []payrollDayVM
		var lineProjects []payrollProjectVM
		if isAdj {
			hours = "—"
			rate = "—"
		} else {
			lineDays = payrollDays(resolveLang(r), days, tracked[l.UserID])
			lineProjects = payrollProjects(resolveLang(r), byProject[l.UserID], projectNames)
		}
		vms = append(vms, payrollLineVM{
			ID: l.ID, Adjustment: isAdj, Under: under, Label: l.Label, Hours: hours, Rate: rate,
			Amount: moneyL(resolveLang(r), l.AmountCents, run.Currency),
			Days:   lineDays, Projects: lineProjects,
		})
	}
	var members []memberOption
	if run.Status == "draft" {
		members = s.memberOptions(r)
	}
	// "Recalculate" opens this preview first: per person, what they get now
	// and what they would get, with the warnings, and nothing written until
	// the manager confirms the new total.
	var preview *payrollRecalcPreviewVM
	if canManage(r) && run.Status == "draft" && r.URL.Query().Get("preview") == "recalculate" {
		res, err := s.services.Payroll.PreviewRecalculation(r.Context(), appmodel.PayrollRecalculateRequest{TeamID: teamID(r), RunID: run.ID, CallerID: authenticatedUserID(r)})
		if err != nil {
			http.Redirect(w, r, "/payroll/"+strconv.FormatInt(run.ID, 10)+"?flash="+encodeFlash(false, s.payrollErrorMessage(r, err)), http.StatusSeeOther)
			return
		}
		lang := resolveLang(r)
		preview = &payrollRecalcPreviewVM{
			Before: moneyL(lang, res.BeforeCents, run.Currency), After: moneyL(lang, res.AfterCents, run.Currency),
			Expect: res.Fingerprint, Changed: res.Changed,
			NoEarnings: res.NoEarnings, NotInRun: res.NotInRun,
		}
		for _, p := range res.People {
			preview.People = append(preview.People, payrollRecalcPersonVM{
				UserID: p.UserID, Name: p.Name, Before: moneyL(lang, p.BeforeCents, run.Currency), After: moneyL(lang, p.AfterCents, run.Currency),
				Delta:        signedMoneyL(lang, p.AfterCents-p.BeforeCents, run.Currency),
				Changed:      p.BeforeCents != p.AfterCents || p.BeforeSeconds != p.AfterSeconds,
				HoursBefore:  fmtHoursL(lang, money.HoursHundredths(p.BeforeSeconds)),
				HoursAfter:   fmtHoursL(lang, money.HoursHundredths(p.AfterSeconds)),
				HoursChanged: p.BeforeSeconds != p.AfterSeconds,
			})
			// A change smaller than a hundredth of an hour still shows what moved.
			if v := &preview.People[len(preview.People)-1]; v.HoursChanged && v.HoursBefore == v.HoursAfter {
				v.HoursBefore, v.HoursAfter = clockSeconds(p.BeforeSeconds), clockSeconds(p.AfterSeconds)
			}
		}
	}
	data := payrollDetailPage{
		pageData:     pageData{Title: run.Number, Active: "payroll", Lang: lang, ReactApp: true},
		PayrollReact: true, PayrollDetail: true,
		Members: members, RecalcPreview: preview,
		Run: payrollVM{
			ID: run.ID, Number: run.Number, Status: run.Status, Notes: run.Notes,
			PeriodLabel: periodSpan(resolveLang(r), fmtDate(resolveLang(r), run.PeriodStart.In(loc)), fmtDate(resolveLang(r), run.PeriodEnd.In(loc).AddDate(0, 0, -1))),
			Lines:       vms, Total: moneyL(resolveLang(r), details.TotalCents, run.Currency), TotalCents: details.TotalCents,
			Hours: fmtHoursL(resolveLang(r), details.TotalHoursHundredths),
			// Money follows approved weeks; naming them turns the run from a
			// bare sum into a traceable document. The timesheet link opens the
			// first week of the period.
			WeeksLabel: payrollWeeksLabel(r, loc, days),
		},
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	s.renderPageForRequest(w, r, run.Number, "payroll", &data)
}

type payrollLineVM struct {
	ID         int64
	Adjustment bool
	// Under is an adjustment shown indented beneath its person's own line.
	Under  bool
	Label  string
	Hours  string
	Rate   string
	Amount string
	// Days is what the person logged on each day of the run, days with no time left out (FIN-18).
	Days     []payrollDayVM
	Projects []payrollProjectVM
}

// payrollDayVM is one day of a payroll line's breakdown.
type payrollDayVM struct {
	Day   string
	Hours string
}

type payrollProjectVM struct {
	Project string
	Hours   string
}

// maxPayrollBreakdownDays keeps the per-day query bounded: a run longer than this shows
// its totals only.
const maxPayrollBreakdownDays = 370

// runDays lists the days a run covers. PeriodEnd is exclusive.
func runDays(start, end time.Time) []time.Time {
	var days []time.Time
	for day := start; day.Before(end); day = day.AddDate(0, 0, 1) {
		if len(days) >= maxPayrollBreakdownDays {
			return nil
		}
		days = append(days, day)
	}
	return days
}

// payrollDays pairs a person's tracked seconds with the run's days, dropping the empty ones.
func payrollDays(lang i18n.Lang, days []time.Time, seconds []int) []payrollDayVM {
	var out []payrollDayVM
	for i, day := range days {
		if i >= len(seconds) || seconds[i] <= 0 {
			continue
		}
		out = append(out, payrollDayVM{Day: fmtDay(lang, day), Hours: fmtHoursL(lang, money.HoursHundredths(seconds[i]))})
	}
	return out
}

// payrollWeeksLabel names the distinct calendar weeks of a run's days: the
// run's money is approved weeks, and the label lets a person check which
// ones without opening the timesheet. A week is named by its first day.
func payrollWeeksLabel(r *http.Request, loc *time.Location, days []time.Time) string {
	if len(days) == 0 {
		return ""
	}
	sunday := weekStartsSunday(r)
	firstDay := func(day time.Time) time.Time {
		start := weekStartOn(sunday, day)
		return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	}
	var labels []string
	var last time.Time
	for _, day := range days {
		start := firstDay(day)
		if !last.IsZero() && start.Equal(last) {
			continue
		}
		last = start
		labels = append(labels, fmtDay(resolveLang(r), start))
		if len(labels) >= 4 {
			break
		}
	}
	return strings.Join(labels, ", ")
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
	// WeeksLabel names the calendar weeks the run's hours came from, so the
	// payout can be traced back to its approved weeks.
	WeeksLabel string
}

type payrollDetailPage struct {
	pageData
	PayrollReact  bool
	PayrollDetail bool
	Run           payrollVM
	Members       []memberOption
	RecalcPreview *payrollRecalcPreviewVM `json:",omitempty"`
	Flash         string
	FlashOK       bool
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
	http.Redirect(w, r, "/payroll?flash=run_deleted", http.StatusSeeOther)
}

func parseSignedCents(raw string) (int, error) {
	v := strings.NewReplacer(" ", "", "\u00a0", "", "\u202f", "", ",", ".").Replace(strings.TrimSpace(raw))
	if v == "" {
		return 0, errors.New("empty amount")
	}
	sign := 1
	if strings.HasPrefix(v, "-") {
		sign = -1
		v = v[1:]
	} else if strings.HasPrefix(v, "+") {
		v = v[1:]
	}
	cents, err := money.ParseCents(v)
	if err != nil || cents == 0 {
		return 0, errors.New("bad amount")
	}
	return sign * cents, nil
}

// handlePayrollAddAdjustment appends a bonus or deduction line to a draft run.
func (s *Server) handlePayrollAddAdjustment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.invalid form")), http.StatusSeeOther)
		return
	}
	uid, err := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
	if err != nil || uid <= 0 {
		http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "pay.adjBad")), http.StatusSeeOther)
		return
	}
	rawAmount := strings.TrimSpace(r.PostForm.Get("amount"))
	isPercent := r.PostForm.Get("unit") == "percent" || strings.HasSuffix(rawAmount, "%")
	req := appmodel.PayrollAdjustmentRequest{
		TeamID: teamID(r), RunID: id, CallerID: authenticatedUserID(r), UserID: uid,
	}
	if isPercent {
		bps, err := parseSignedPercentBps(rawAmount)
		if err != nil {
			http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "pay.adjBad")), http.StatusSeeOther)
			return
		}
		req.PercentBps = bps
	} else {
		cents, err := parseSignedCents(rawAmount)
		if err != nil {
			http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "pay.adjBad")), http.StatusSeeOther)
			return
		}
		req.AmountCents = cents
	}
	label := strings.TrimSpace(r.PostForm.Get("label"))
	if label == "" {
		http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(false, i18n.T(resolveLang(r), "pay.adjBad")), http.StatusSeeOther)
		return
	}
	req.Label = label
	if err := s.services.Payroll.AddAdjustment(r.Context(), req); err != nil {
		msg := s.payrollErrorMessage(r, err)
		http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(true, "adjustment added"), http.StatusSeeOther)
}

// handlePayrollDeleteAdjustment removes a one-off adjustment line from a draft run.
func (s *Server) handlePayrollDeleteAdjustment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	lineID, err := strconv.ParseInt(r.PathValue("line"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.services.Payroll.DeleteAdjustment(r.Context(), appmodel.PayrollAdjustmentDeleteRequest{
		TeamID: teamID(r), RunID: id, LineID: lineID, CallerID: authenticatedUserID(r),
	}); err != nil {
		msg := s.payrollErrorMessage(r, err)
		http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(false, msg), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/payroll/"+strconv.FormatInt(id, 10)+"?flash="+encodeFlash(true, "adjustment deleted"), http.StatusSeeOther)
}

// handlePayrollRecalculate prices a draft run again after the timesheet changed.
func (s *Server) handlePayrollRecalculate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := "/payroll/" + strconv.FormatInt(id, 10)
	// Only a confirmed preview applies: without its fingerprint (an old tab,
	// a script) the request is answered with a fresh preview.
	req := appmodel.PayrollRecalculateRequest{TeamID: teamID(r), RunID: id, CallerID: authenticatedUserID(r), Expect: strings.TrimSpace(r.FormValue("expect"))}
	if req.Expect == "" {
		// Only a draft has a preview; for anything else the plain error
		// below says it is not a draft.
		detail, err := s.services.Payroll.GetRun(r.Context(), teamID(r), id)
		if err != nil {
			http.Redirect(w, r, back+"?flash="+encodeFlash(false, s.payrollErrorMessage(r, err)), http.StatusSeeOther)
			return
		}
		if detail.Run.Status != "draft" {
			http.Redirect(w, r, back+"?flash="+encodeFlash(false, s.payrollErrorMessage(r, model.ErrPayrollNotDraft)), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, back+"?preview=recalculate&flash="+encodeFlash(false, i18n.T(resolveLang(r), "pay.recalcConfirm")), http.StatusSeeOther)
		return
	}
	result, err := s.services.Payroll.RecalculateDraft(r.Context(), req)
	if errors.Is(err, model.ErrPayrollChanged) {
		// The numbers moved while the preview was open: show the new ones.
		http.Redirect(w, r, back+"?preview=recalculate&flash="+encodeFlash(false, i18n.T(resolveLang(r), "pay.recalcChanged")), http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, back+"?flash="+encodeFlash(false, s.payrollErrorMessage(r, err)), http.StatusSeeOther)
		return
	}
	// The flash says what moved: the total before and after, and who has no
	// earnings left, since their percent bonus silently became zero.
	lang := resolveLang(r)
	currency := ""
	if detail, err := s.services.Payroll.GetRun(r.Context(), teamID(r), id); err == nil {
		currency = detail.Run.Currency
	}
	msg := fmt.Sprintf(i18n.T(lang, "pay.recalculated"), moneyL(lang, result.BeforeCents, currency), moneyL(lang, result.AfterCents, currency))
	if len(result.NoEarnings) > 0 {
		msg += " " + fmt.Sprintf(i18n.T(lang, "pay.recalculatedNoEarnings"), strings.Join(result.NoEarnings, ", "))
	}
	http.Redirect(w, r, back+"?flash="+encodeFlash(true, msg), http.StatusSeeOther)
}

// handleMemberPay saves a member's pay rate + daily capacity.
func (s *Server) handleMemberPay(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, i18n.T(resolveLang(r), "flash.msg.invalid form")), http.StatusSeeOther)
		return
	}
	uid, err := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, "bad user"), http.StatusSeeOther)
		return
	}
	caller, ok := UserFrom(r.Context()) // the manage middleware authenticates the manager
	if !ok {
		http.Error(w, i18n.T(resolveLang(r), "err.unauthorized"), http.StatusUnauthorized)
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
	// The pay kind (FIN-26): the select travels on every save of this form, so
	// a save also pins the member's kind; both money fields travel with it.
	// An older caller that sends no kind keeps the kind as it is.
	var kind *string
	switch v := r.PostForm.Get("pay_kind"); v {
	case "hourly", "monthly":
		kind = &v
	case "":
	default:
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, "bad pay kind"), http.StatusSeeOther)
		return
	}
	var monthly *int
	if n, has, err := formCents(r, "monthly_pay"); err != nil {
		http.Redirect(w, r, "/settings/members?flash="+encodeFlash(false, i18n.T(resolveLang(r), "bill.badRate")), http.StatusSeeOther)
		return
	} else if has {
		monthly = &n
	}
	if err := s.services.Payroll.UpdateMemberPay(operationContext(r), appmodel.PayrollMemberPayRequest{TeamID: teamID(r), UserID: uid, CallerID: caller.ID, PayCents: pay, CapacityMinutes: cap, Kind: kind, MonthlyPayCents: monthly}); err != nil {
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

// markPeriodDuplicates names, on every run of a period but the first in the
// list, the run it repeats. The list is newest first, so a repeat carries the
// number of the older document the period is now shared with.
func markPeriodDuplicates(items []payrollSummary) {
	firstByPeriod := map[string]string{}
	for i, item := range items {
		if first, seen := firstByPeriod[item.Period]; seen {
			items[i].DuplicateOf = first
			continue
		}
		firstByPeriod[item.Period] = item.Number
	}
}

func (s *Server) payrollMemberOptions(r *http.Request) []payrollMemberOption {
	snapshot, err := s.services.MemberAdmin.Management(r.Context(), teamID(r))
	if err != nil {
		return nil
	}
	kindByID := make(map[int64]string, len(snapshot.PaySettings))
	for _, ps := range snapshot.PaySettings {
		if ps.Kind != "" {
			kindByID[ps.UserID] = ps.Kind
		}
	}
	out := make([]payrollMemberOption, 0, len(snapshot.Members))
	for _, member := range snapshot.Members {
		name := member.Name
		if name == "" {
			name = member.Email
		}
		kind := kindByID[member.UserID]
		if kind == "" {
			kind = "hourly"
		}
		out = append(out, payrollMemberOption{ID: member.UserID, Name: name, PayKind: kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func parsePostFormIDs(raw []string) []int64 {
	var out []int64
	seen := map[int64]bool{}
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id > 0 && !seen[id] {
				out = append(out, id)
				seen[id] = true
			}
		}
	}
	return out
}

func parseIDsParam(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return parsePostFormIDs([]string{raw})
}

func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

func parseSignedPercentBps(raw string) (int, error) {
	v := strings.NewReplacer(" ", "", "\u00a0", "", "\u202f", "", "%", "", ",", ".").Replace(strings.TrimSpace(raw))
	if v == "" {
		return 0, errors.New("empty percent")
	}
	sign := 1
	if strings.HasPrefix(v, "-") {
		sign = -1
		v = v[1:]
	} else if strings.HasPrefix(v, "+") {
		v = v[1:]
	}
	cents, err := money.ParseCents(v)
	if err != nil || cents == 0 {
		return 0, errors.New("bad percent")
	}
	return sign * cents, nil
}

func payrollProjects(lang i18n.Lang, byProj map[int64]int, names map[int64]string) []payrollProjectVM {
	if len(byProj) == 0 {
		return nil
	}
	type pair struct {
		name string
		secs int
	}
	var pairs []pair
	for pid, secs := range byProj {
		if secs <= 0 {
			continue
		}
		name := names[pid]
		if name == "" {
			name = i18n.T(lang, "stats.uncategorized")
		}
		pairs = append(pairs, pair{name: name, secs: secs})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].secs != pairs[j].secs {
			return pairs[i].secs > pairs[j].secs
		}
		return pairs[i].name < pairs[j].name
	})
	out := make([]payrollProjectVM, len(pairs))
	for i, p := range pairs {
		out[i] = payrollProjectVM{Project: p.name, Hours: fmtHoursL(lang, money.HoursHundredths(p.secs))}
	}
	return out
}

// payrollRecalcPreviewVM is what a recalculation would change, shown before it
// is applied.
type payrollRecalcPreviewVM struct {
	Before     string
	After      string
	Expect     string
	Changed    bool
	People     []payrollRecalcPersonVM
	NoEarnings []string
	NotInRun   []string
}

type payrollRecalcPersonVM struct {
	UserID       int64
	HoursBefore  string
	HoursAfter   string
	HoursChanged bool
	Name         string
	Before       string
	After        string
	Delta        string
	Changed      bool
}

// signedMoneyL writes a difference with its sign, "+1 200,00 ₽" or "−300,00 ₽".
func signedMoneyL(lang i18n.Lang, cents int, currency string) string {
	switch {
	case cents > 0:
		return "+" + moneyL(lang, cents, currency)
	case cents < 0:
		return "−" + moneyL(lang, -cents, currency)
	}
	return moneyL(lang, 0, currency)
}

// clockSeconds writes a duration to the second, "1:00:30", for an hours
// change too small to show in hundredths of an hour.
func clockSeconds(secs int) string {
	return fmt.Sprintf("%d:%02d:%02d", secs/3600, secs%3600/60, secs%60)
}
