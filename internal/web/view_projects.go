package web

import (
	"cmp"
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
)

// projectDetailData is the presentation envelope for /projects/{slug}.
type projectDetailData struct {
	Title           string
	Active          string
	ReactApp        bool
	Project         projectView
	Activities      []activityView
	Sessions        []sessionView
	Total           string
	MonthTotal      string
	Archived        bool
	EstimateLabel   string
	EstimateInput   string
	EstimatePercent int
	RateInput       string
	RateLabel       string // "2 500 ₽ / ч" for managers; empty when unset
	Currency        string // project's own ('' = workspace's)
	TeamCurrency    string
	Currencies      []currencyOption
	Flash           string
	FlashOK         bool
	CSRFToken       string
	Lang            string
	Unbilled        []unbilledView
	CanManage       bool // rates, client and settings are for managers only
	Client          *clientRef
	ClientOptions   []clientRef
	Budget          budgetView
}

// clientRef is the picker's option shape for linking a project to a card.
type clientRef struct {
	ID   int64
	Name string
}

// budgetView is the money view of a project ceiling.
//
// Every figure here is a measurement of tracked time, never an estimate: the
// product promise is that the registry is what actually happened, and a
// budget read off an estimate would quietly break it.
type budgetView struct {
	// BudgetInput is the ceiling pre-filled in the edit form, blank when
	// unset. Without it the field would render empty and look like the
	// project had no budget even after one was saved.
	BudgetInput string
	HasRate     bool
	HasBudget   bool
	// Percent is the measured share of the ceiling, already clamped at 100
	// for the bar; OverByLabel carries the overflow itself.
	Percent int
	// CurrencySourceLabel names the rule that chose the currency, so a
	// manager can tell "bills in dollars because the client says so" from
	// "because the workspace defaults to dollars".
	CurrencySourceLabel string
	BudgetLabel         string
	SpentLabel          string
	UnbilledLabel       string
	RemainingLabel      string
	Over                bool
	OverByLabel         string
	Currency            string
}

// projectDetailExtras carries what the detail page needs but the project
// snapshot does not know about: the client directory and the budget read.
type projectDetailExtras struct {
	Client        *clientRef
	ClientOptions []clientRef
	Budget        budgetView
}

func (projectDetailData) isTemplateData()    {}
func (projectDetailData) usesReactApp() bool { return true }

func (p *projectDetailData) setCSRF(t string)   { p.CSRFToken = t }
func (p *projectDetailData) setManage(v bool)   { p.CanManage = v }
func (p *projectDetailData) setLang(l string)   { p.Lang = l }
func (p projectDetailData) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

func (s *Server) buildProjectDetailPage(r *http.Request, snapshot appmodel.ProjectPageSnapshot, extras projectDetailExtras) projectDetailData {
	detail := snapshot.Detail
	p := detail.Project
	now := userNow(r)
	from := now.Add(-30 * 24 * time.Hour)
	activity := detail.Activity
	sessions := make([]sessionView, 0, min(len(activity.Recent), 50))
	for _, item := range activity.Recent {
		view := toSessionView(item.Session, item.Activity, from, now, now, resolveLang(r), durFmtOf(r))
		if len(sessions) < 50 {
			sessions = append(sessions, view)
		}
	}
	totalSeconds := activity.TotalSeconds // all history, tracked time with pauses excluded
	recentSeconds := activity.RecentSeconds
	// A running timer counts as it runs here too, like the timesheet.
	for _, item := range s.runningSessions(r) {
		if item.Activity.ProjectID == p.ID {
			totalSeconds += item.Session.TrackedSecondsInWindow(time.Time{}, now, now)
			recentSeconds += item.Session.TrackedSecondsInWindow(from, now, now)
		}
	}
	attachSessionTags(sessions, snapshot.TagsBySession)
	attachSessionProjects(sessions, snapshot.ProjectsByID)

	estimateLabel, estimateInput, estimatePercent, rateInput := "", "", 0, ""
	if p.EstimateMinutes != nil && *p.EstimateMinutes > 0 {
		estimateLabel = fmtMinutesL(resolveLang(r), *p.EstimateMinutes)
		estimateInput = strconv.Itoa(*p.EstimateMinutes)
		estimatePercent = detail.EstimatePercent
	}
	if p.BillableRateCents != nil && canManage(r) {
		rateInput = formatMoneyInput(resolveLang(r), *p.BillableRateCents)
	}
	data := projectDetailData{
		Title: p.Name, Active: "projects", ReactApp: true,
		Project: projectView{
			ID: p.ID, Slug: p.Slug, Name: p.Name, Color: p.Color,
			Archived: p.Archived, Billable: p.Billable,
		},
		Activities: activityViews(detail.Activities, string(resolveLang(r))), Sessions: sessions,
		Total: fmtDur(r, totalSeconds), MonthTotal: fmtDur(r, recentSeconds),
		Archived: p.Archived, EstimateLabel: estimateLabel, EstimateInput: estimateInput,
		RateInput: rateInput, EstimatePercent: estimatePercent, Currencies: currencyOptions(),
	}
	data.Currency = detail.Currency
	if canManage(r) && s.teamModules(r)["invoices"] {
		data.Unbilled = unbilledViewsFrom(snapshot.Unbilled, r)
	}
	data.TeamCurrency = snapshot.TeamCurrency
	data.Client = extras.Client
	data.ClientOptions = extras.ClientOptions
	data.Budget = extras.Budget
	if p.BillableRateCents != nil && canManage(r) {
		// The budget already resolved client → project → workspace currency.
		cur := data.Budget.Currency
		if cur == "" {
			cur = cmp.Or(detail.Currency, data.TeamCurrency)
		}
		lang := resolveLang(r)
		data.RateLabel = moneyL(lang, *p.BillableRateCents, cur) + " / " + i18n.T(lang, "inv.hoursShort")
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	return data
}
