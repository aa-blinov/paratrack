package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
	"github.com/aa-blinov/paratrack/internal/money"
)

// projectDetailData is the presentation envelope for /projects/{slug}.
type projectDetailData struct {
	Title           string
	Active          string
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
	Currency        string // project's own ('' = workspace's)
	TeamCurrency    string
	Currencies      []currencyOption
	Flash           string
	FlashOK         bool
	CSRFToken       string
	Lang            string
	Unbilled        []unbilledView
	CanManage       bool // rates, client and settings are for managers only
}

func (projectDetailData) isTemplateData() {}

func (p *projectDetailData) setCSRF(t string)   { p.CSRFToken = t }
func (p *projectDetailData) setManage(v bool)   { p.CanManage = v }
func (p *projectDetailData) setLang(l string)   { p.Lang = l }
func (p projectDetailData) T(key string) string { return i18n.T(i18n.Lang(p.Lang), key) }

func (s *Server) buildProjectDetailPage(r *http.Request, p model.Project) (projectDetailData, error) {
	tid := teamID(r)
	includeArchived := r.URL.Query().Get("archived") == "1"
	activities, err := s.services.Projects.Queries.Activities(r.Context(), tid, p.ID, includeArchived)
	if err != nil {
		return projectDetailData{}, fmt.Errorf("list project activities: %w", err)
	}

	// Recent sessions in this project (last 30 days, capped at 50).
	now := userNow(r)
	from := now.Add(-30 * 24 * time.Hour)
	activity, err := s.services.Projects.Queries.Activity(r.Context(), tid, p.ID, from, now)
	if err != nil {
		return projectDetailData{}, fmt.Errorf("load project activity summary: %w", err)
	}
	sessions := make([]sessionView, 0, min(len(activity.Recent), 50))
	monthSeconds := 0
	for _, item := range activity.Recent {
		view := toSessionView(item.Session, item.Activity, from, now, now, resolveLang(r), durFmtOf(r))
		monthSeconds, err = money.AddInt(monthSeconds, view.DurationSecs)
		if err != nil {
			return projectDetailData{}, fmt.Errorf("sum recent project time: %w", err)
		}
		if len(sessions) < 50 {
			sessions = append(sessions, view)
		}
	}
	totalSeconds := activity.TotalSeconds // all history, tracked time with pauses excluded
	hydrateSessionTags(r.Context(), s.services.SessionTags, tid, sessions, s.logger)
	hydrateSessionProjects(r.Context(), s.services.Projects.Queries, tid, sessions, s.logger)

	estimateLabel, estimateInput, estimatePercent, rateInput := "", "", 0, ""
	if p.EstimateMinutes != nil && *p.EstimateMinutes > 0 {
		estimateLabel = fmtMinutesL(resolveLang(r), *p.EstimateMinutes)
		estimateInput = strconv.Itoa(*p.EstimateMinutes)
		estimatePercent = money.PercentRatio(totalSeconds, *p.EstimateMinutes, 5, 3)
	}
	if p.BillableRateCents != nil && canManage(r) {
		rateInput = formatMoneyInput(resolveLang(r), *p.BillableRateCents)
	}
	data := projectDetailData{
		Title: p.Name, Active: "projects",
		Project: projectView{
			ID: p.ID, Slug: p.Slug, Name: p.Name, Color: p.Color,
			Archived: p.Archived, Billable: p.Billable,
		},
		Activities: activityViews(activities, string(resolveLang(r))), Sessions: sessions,
		Total: fmtDur(r, totalSeconds), MonthTotal: fmtDur(r, monthSeconds),
		Archived: p.Archived, EstimateLabel: estimateLabel, EstimateInput: estimateInput,
		RateInput: rateInput, EstimatePercent: estimatePercent, Currencies: currencyOptions(),
	}
	data.Currency, err = s.services.Projects.Queries.Currency(r.Context(), tid, p.ID)
	if err != nil {
		return projectDetailData{}, fmt.Errorf("load project currency: %w", err)
	}
	if canManage(r) && s.teamModules(r)["invoices"] {
		data.Unbilled, err = s.unbilledViews(r, p.ID)
		if err != nil {
			return projectDetailData{}, fmt.Errorf("load project unbilled time: %w", err)
		}
	}
	data.TeamCurrency, err = s.services.Teams.Settings.Currency(r.Context(), tid)
	if err != nil {
		return projectDetailData{}, fmt.Errorf("load workspace currency: %w", err)
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	return data, nil
}
