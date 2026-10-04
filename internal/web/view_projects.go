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

func (s *Server) buildProjectDetailPage(r *http.Request, detail model.ProjectDetail) (projectDetailData, error) {
	p := detail.Project
	tid := teamID(r)
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
		Activities: activityViews(detail.Activities, string(resolveLang(r))), Sessions: sessions,
		Total: fmtDur(r, totalSeconds), MonthTotal: fmtDur(r, activity.RecentSeconds),
		Archived: p.Archived, EstimateLabel: estimateLabel, EstimateInput: estimateInput,
		RateInput: rateInput, EstimatePercent: estimatePercent, Currencies: currencyOptions(),
	}
	data.Currency = detail.Currency
	var err error
	if canManage(r) && s.teamModules(r)["invoices"] {
		data.Unbilled, err = s.unbilledViews(r, p.ID)
		if err != nil {
			return projectDetailData{}, fmt.Errorf("load project unbilled time: %w", err)
		}
	}
	teamCurrency, err := s.services.Teams.Settings.Currency(r.Context(), tid)
	if err != nil {
		return projectDetailData{}, fmt.Errorf("load workspace currency: %w", err)
	}
	data.TeamCurrency = teamCurrency
	if flash := r.URL.Query().Get("flash"); flash != "" {
		data.Flash, data.FlashOK = decodeFlash(flash, resolveLang(r))
	}
	return data, nil
}
