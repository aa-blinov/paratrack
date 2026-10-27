package web

import (
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The views below are the JSON shapes of CRM and calendar entities. The React
// screens and the MCP tools read the same shapes. Zero ids mean "none", and dates
// are YYYY-MM-DD in the workspace day, empty when unset.

type crmDealView struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Stage     string `json:"stage"`
	ClientID  int64  `json:"client_id"`
	ContactID int64  `json:"contact_id"`
	OwnerID   int64  `json:"owner_id"`
	// AmountVisible says whether this answer carries the amount and the currency. Only a
	// manager sees them; a member's answer leaves both out and says so here.
	AmountVisible bool      `json:"amount_visible"`
	AmountCents   int64     `json:"amount_cents,omitempty"`
	Currency      string    `json:"currency,omitempty"`
	Probability   int       `json:"probability"`
	CloseDate     string    `json:"close_date"`
	NextStepAt    string    `json:"next_step_at"`
	LostReason    string    `json:"lost_reason"`
	ProjectID     int64     `json:"project_id"`
	Overdue       bool      `json:"overdue"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (crmDealView) isJSONResponse() {}

// crmDealViewOf shapes a deal for the caller. manager is the caller's role as the request
// read it, never a value the client sent.
func crmDealViewOf(d model.CRMDeal, today string, manager bool) crmDealView {
	v := crmDealView{
		ID: d.ID, Title: d.Title, Stage: d.Stage, ClientID: d.ClientID, ContactID: d.ContactID,
		OwnerID: d.OwnerID, AmountVisible: manager, Probability: d.Probability,
		CloseDate: d.CloseDate, NextStepAt: d.NextStepAt, LostReason: d.LostReason, ProjectID: d.ProjectID,
		Overdue: d.Overdue(today), UpdatedAt: d.UpdatedAt,
	}
	if manager {
		v.AmountCents, v.Currency = d.AmountCents, d.Currency
	}
	return v
}

// stageName is the name a reader sees. A stage nobody named shows its built-in name
// in the reader's language.
func stageName(lang i18n.Lang, key, name string) string {
	if name != "" {
		return name
	}
	return i18n.T(lang, "crm.stage."+key)
}

// crmStageView is one stage of the pipeline with the number of deals in it.
type crmStageView struct {
	Stage string `json:"stage"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// crmStageConfigView is a stage as the pipeline editor and the agents read it.
type crmStageConfigView struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Position int    `json:"position"`
	Kind     string `json:"kind"`
}

func (crmStageConfigView) isJSONResponse() {}

func crmStageConfigViewOf(s model.CRMStage, lang i18n.Lang) crmStageConfigView {
	return crmStageConfigView{Key: s.Key, Name: stageName(lang, s.Key, s.Name), Position: s.Position, Kind: s.Kind}
}

type crmStagesResponse struct {
	Stages []crmStageConfigView `json:"stages"`
}

func (crmStagesResponse) isJSONResponse() {}

func crmStageConfigViews(stages []model.CRMStage, lang i18n.Lang) []crmStageConfigView {
	out := make([]crmStageConfigView, 0, len(stages))
	for _, stage := range stages {
		out = append(out, crmStageConfigViewOf(stage, lang))
	}
	return out
}

type crmPipelineView struct {
	Stages  []crmStageView `json:"stages"`
	Overdue int            `json:"overdue"`
}

type crmDealsResponse struct {
	Today    string          `json:"today"`
	Deals    []crmDealView   `json:"deals"`
	Pipeline crmPipelineView `json:"pipeline"`
}

func (crmDealsResponse) isJSONResponse() {}

type crmContactView struct {
	ID       int64  `json:"id"`
	ClientID int64  `json:"client_id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	// CanDelete and CanEdit are set on the workspace screen only, where the caller's rights
	// are known. Both follow the rule that removes a contact: a manager, or its creator.
	CanDelete bool `json:"can_delete,omitempty"`
	CanEdit   bool `json:"can_edit,omitempty"`
}

func (crmContactView) isJSONResponse() {}

func crmContactViewOf(c model.CRMContact) crmContactView {
	return crmContactView{ID: c.ID, ClientID: c.ClientID, Name: c.Name, Role: c.Role, Email: c.Email, Phone: c.Phone}
}

// crmContactViewFor is the contact as the caller may see it: a manager, or the member who
// created the contact, may delete it.
func crmContactViewFor(r *http.Request, c model.CRMContact) crmContactView {
	view := crmContactViewOf(c)
	view.CanDelete = canManage(r) || c.CreatedBy == authenticatedUserID(r)
	view.CanEdit = view.CanDelete
	return view
}

type crmContactsResponse struct {
	Contacts []crmContactView `json:"contacts"`
}

func (crmContactsResponse) isJSONResponse() {}

type crmActivityView struct {
	ID         int64     `json:"id"`
	ClientID   int64     `json:"client_id"`
	DealID     int64     `json:"deal_id"`
	LeadID     int64     `json:"lead_id"`
	Kind       string    `json:"kind"`
	Body       string    `json:"body"`
	Result     string    `json:"result"`
	OccurredAt time.Time `json:"occurred_at"`
	AuthorID   int64     `json:"author_id"`
}

func (crmActivityView) isJSONResponse() {}

// crmActivityViewIn shows the moment in the workspace zone, like the calendar does.
func crmActivityViewIn(a model.CRMActivity, loc *time.Location) crmActivityView {
	return crmActivityView{
		ID: a.ID, ClientID: a.ClientID, DealID: a.DealID, LeadID: a.LeadID, Kind: a.Kind, Body: a.Body,
		Result: a.Result, OccurredAt: a.OccurredAt.In(loc), AuthorID: a.AuthorID,
	}
}

type crmActivitiesResponse struct {
	Activities []crmActivityView `json:"activities"`
}

func (crmActivitiesResponse) isJSONResponse() {}

// crmLeadView is a lead as the screens and the agents read it. A converted lead names the
// client and the deal it made; zero means none yet.
type crmLeadView struct {
	ID                int64     `json:"id"`
	Company           string    `json:"company"`
	ContactName       string    `json:"contact_name"`
	Phone             string    `json:"phone"`
	Email             string    `json:"email"`
	Source            string    `json:"source"`
	Message           string    `json:"message"`
	OwnerID           int64     `json:"owner_id"`
	Status            string    `json:"status"`
	LostReason        string    `json:"lost_reason"`
	NextStepAt        string    `json:"next_step_at"`
	ConvertedDealID   int64     `json:"converted_deal_id"`
	ConvertedClientID int64     `json:"converted_client_id"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	// The first response (CRM-16). ResponseMinutes is the time to the first activity, zero
	// until there is one; WaitingMinutes is the age of a lead that still has none; WithinTarget
	// is set when the first activity came within the target. A response of zero minutes with
	// WithinTarget false therefore means waiting, since a late answer is always above zero.
	ResponseMinutes int64 `json:"response_minutes"`
	WaitingMinutes  int64 `json:"waiting_minutes"`
	WithinTarget    bool  `json:"within_target"`
}

func (crmLeadView) isJSONResponse() {}

// crmLeadViewOf shapes a lead. now is the moment a waiting lead's age is counted to.
func crmLeadViewOf(l model.CRMLead, now time.Time) crmLeadView {
	v := crmLeadView{
		ID: l.ID, Company: l.Company, ContactName: l.ContactName, Phone: l.Phone, Email: l.Email,
		Source: l.Source, Message: l.Message, OwnerID: l.OwnerID, Status: l.Status, LostReason: l.LostReason,
		NextStepAt: l.NextStepAt, ConvertedDealID: l.ConvertedDealID, ConvertedClientID: l.ConvertedClientID,
		CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
	if l.FirstResponseAt.IsZero() {
		v.WaitingMinutes = max(0, int64(now.Sub(l.CreatedAt)/time.Minute))
		return v
	}
	response := l.ResponseTime()
	v.ResponseMinutes = int64(response / time.Minute)
	v.WithinTarget = response <= model.LeadResponseTargetMinutes*time.Minute
	return v
}

func crmLeadViews(leads []model.CRMLead, now time.Time) []crmLeadView {
	out := make([]crmLeadView, 0, len(leads))
	for _, l := range leads {
		out = append(out, crmLeadViewOf(l, now))
	}
	return out
}

// crmLeadResponseStatsView is the first-response measure of a period (CRM-16). From and To
// are the days the caller asked for. MedianMinutes is null while no lead of the period has an
// activity.
type crmLeadResponseStatsView struct {
	From          string `json:"from"`
	To            string `json:"to"`
	TargetMinutes int64  `json:"target_minutes"`
	Created       int64  `json:"created"`
	Answered      int64  `json:"answered"`
	WithinTarget  int64  `json:"within_target"`
	MedianMinutes *int64 `json:"median_minutes"`
}

func (crmLeadResponseStatsView) isJSONResponse() {}

func crmLeadResponseStatsViewOf(stats model.CRMLeadResponseStats, from, to string) crmLeadResponseStatsView {
	v := crmLeadResponseStatsView{
		From: from, To: to, TargetMinutes: model.LeadResponseTargetMinutes,
		Created: stats.Created, Answered: stats.Answered, WithinTarget: stats.WithinTarget,
	}
	if stats.Answered > 0 {
		median := stats.MedianMinutes
		v.MedianMinutes = &median
	}
	return v
}

// crmFunnelView is the funnel report of a period (CRM-17). AmountsVisible says whether the
// caller is a manager; a member's totals carry no amounts, and the amount lists are left out.
// A conversion is null while nothing was closed or created, not zero.
type crmFunnelView struct {
	From           string                   `json:"from"`
	To             string                   `json:"to"`
	Today          string                   `json:"today"`
	AmountsVisible bool                     `json:"amounts_visible"`
	Pipeline       []crmFunnelStageView     `json:"pipeline"`
	Won            crmFunnelTotalView       `json:"won"`
	Lost           crmFunnelTotalView       `json:"lost"`
	Conversion     *float64                 `json:"conversion"`
	Overdue        crmFunnelTotalView       `json:"overdue"`
	Leads          crmFunnelLeadsView       `json:"leads"`
	Response       crmLeadResponseStatsView `json:"response"`
}

func (crmFunnelView) isJSONResponse() {}

type crmFunnelStageView struct {
	Stage   string          `json:"stage"`
	Name    string          `json:"name"`
	Kind    string          `json:"kind"`
	Count   int64           `json:"count"`
	Amounts []crmAmountView `json:"amounts,omitempty"`
}

type crmFunnelTotalView struct {
	Count   int64           `json:"count"`
	Amounts []crmAmountView `json:"amounts,omitempty"`
}

type crmAmountView struct {
	Currency    string `json:"currency"`
	AmountCents int64  `json:"amount_cents"`
}

type crmFunnelLeadsView struct {
	Statuses   []crmLeadStatusCountView `json:"statuses"`
	Created    int64                    `json:"created"`
	Converted  int64                    `json:"converted"`
	Conversion *float64                 `json:"conversion"`
	Sources    []crmLeadSourceView      `json:"sources"`
}

type crmLeadStatusCountView struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

// crmLeadSourceView is one source of the period's leads. Label is the source as typed, or the
// words for a lead whose source is empty, in the reader's language.
type crmLeadSourceView struct {
	Source string `json:"source"`
	Label  string `json:"label"`
	Count  int64  `json:"count"`
}

// crmFunnelViewOf shapes the report. from and to are the days the caller asked for, today the
// workspace day the overdue steps were counted against.
func crmFunnelViewOf(report model.CRMFunnel, lang i18n.Lang, from, to, today string) crmFunnelView {
	v := crmFunnelView{
		From: from, To: to, Today: today, AmountsVisible: report.ShowAmounts,
		Pipeline: make([]crmFunnelStageView, 0, len(report.Pipeline)),
		Won:      crmFunnelTotalViewOf(report.Won), Lost: crmFunnelTotalViewOf(report.Lost),
		Overdue:  crmFunnelTotalViewOf(report.Overdue),
		Response: crmLeadResponseStatsViewOf(report.Response, from, to),
	}
	for _, stage := range report.Pipeline {
		v.Pipeline = append(v.Pipeline, crmFunnelStageView{
			Stage: stage.Key, Name: stageName(lang, stage.Key, stage.Name), Kind: stage.Kind,
			Count: stage.Total.Count, Amounts: crmAmountViews(stage.Total.Amounts),
		})
	}
	v.Conversion = ratioOf(v.Won.Count, v.Won.Count+v.Lost.Count)
	v.Leads = crmFunnelLeadsViewOf(report, lang)
	return v
}

func crmFunnelTotalViewOf(total model.CRMDealTotal) crmFunnelTotalView {
	return crmFunnelTotalView{Count: total.Count, Amounts: crmAmountViews(total.Amounts)}
}

func crmAmountViews(amounts []model.CRMAmount) []crmAmountView {
	if len(amounts) == 0 {
		return nil
	}
	out := make([]crmAmountView, 0, len(amounts))
	for _, a := range amounts {
		out = append(out, crmAmountView{Currency: a.Currency, AmountCents: a.AmountCents})
	}
	return out
}

func crmFunnelLeadsViewOf(report model.CRMFunnel, lang i18n.Lang) crmFunnelLeadsView {
	v := crmFunnelLeadsView{
		Statuses:   make([]crmLeadStatusCountView, 0, len(model.LeadStatuses)),
		Created:    report.LeadsCreated,
		Converted:  report.LeadsConverted,
		Conversion: ratioOf(report.LeadsConverted, report.LeadsCreated),
		Sources:    make([]crmLeadSourceView, 0, len(report.LeadSources)),
	}
	for _, status := range model.LeadStatuses {
		v.Statuses = append(v.Statuses, crmLeadStatusCountView{Status: status, Count: report.LeadStatuses[status]})
	}
	for _, source := range report.LeadSources {
		label := source.Source
		if label == "" {
			label = i18n.T(lang, "crm.funnel.noSource")
		}
		v.Sources = append(v.Sources, crmLeadSourceView{Source: source.Source, Label: label, Count: source.Created})
	}
	return v
}

// ratioOf is part over whole, or nil when there is no whole.
func ratioOf(part, whole int64) *float64 {
	if whole == 0 {
		return nil
	}
	ratio := float64(part) / float64(whole)
	return &ratio
}

type crmLeadsResponse struct {
	Leads []crmLeadView `json:"leads"`
}

func (crmLeadsResponse) isJSONResponse() {}

// crmIntakeKeyResponse says whether the workspace has an intake key. Key carries the raw
// secret only in the answer to the request that made it, and that answer is not cached.
type crmIntakeKeyResponse struct {
	Active    bool       `json:"active"`
	CreatedAt *time.Time `json:"created_at,omitempty"`
	Key       string     `json:"key,omitempty"`
}

func (crmIntakeKeyResponse) isJSONResponse() {}

type calendarEventView struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	Location  string    `json:"location"`
	Notes     string    `json:"notes"`
	ProjectID int64     `json:"project_id"`
	ClientID  int64     `json:"client_id"`
	Shared    bool      `json:"shared"`
	// SyncPush is the author's choice to keep a copy in their own provider calendar; Imported
	// marks an event pulled from a provider, which is never pushed back (CRM-19).
	SyncPush       bool    `json:"sync_push"`
	AllDay         bool    `json:"all_day"`
	Imported       bool    `json:"imported"`
	ParticipantIDs []int64 `json:"participant_ids"`
	CreatedBy      int64   `json:"created_by"`
}

func (calendarEventView) isJSONResponse() {}

// calendarEventViewIn shows the times in the workspace zone, so the screens and the
// agent read the same wall clock the person set.
func calendarEventViewIn(e model.CalendarEvent, loc *time.Location) calendarEventView {
	participants := e.Participants
	if participants == nil {
		participants = []int64{}
	}
	return calendarEventView{
		ID: e.ID, Title: e.Title, StartsAt: e.StartsAt.In(loc), EndsAt: e.EndsAt.In(loc), Location: e.Location,
		Notes: e.Notes, ProjectID: e.ProjectID, ClientID: e.ClientID, Shared: e.Shared,
		SyncPush: e.SyncPush, AllDay: e.AllDay, Imported: e.Imported, ParticipantIDs: participants, CreatedBy: e.CreatedBy,
	}
}

func calendarEventViewsIn(events []model.CalendarEvent, loc *time.Location) []calendarEventView {
	out := make([]calendarEventView, 0, len(events))
	for _, e := range events {
		out = append(out, calendarEventViewIn(e, loc))
	}
	return out
}

type calendarEventsResponse struct {
	Events []calendarEventView `json:"events"`
}

func (calendarEventsResponse) isJSONResponse() {}
