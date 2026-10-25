package web

import (
	"time"

	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// The views below are the JSON shapes of CRM and calendar entities. The React
// screens and the MCP tools read the same shapes. Zero ids mean "none", and dates
// are YYYY-MM-DD in the workspace day, empty when unset.

type crmDealView struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Stage       string    `json:"stage"`
	ClientID    int64     `json:"client_id"`
	ContactID   int64     `json:"contact_id"`
	OwnerID     int64     `json:"owner_id"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	Probability int       `json:"probability"`
	CloseDate   string    `json:"close_date"`
	NextStepAt  string    `json:"next_step_at"`
	LostReason  string    `json:"lost_reason"`
	ProjectID   int64     `json:"project_id"`
	Overdue     bool      `json:"overdue"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (crmDealView) isJSONResponse() {}

func crmDealViewOf(d model.CRMDeal, today string) crmDealView {
	return crmDealView{
		ID: d.ID, Title: d.Title, Stage: d.Stage, ClientID: d.ClientID, ContactID: d.ContactID,
		OwnerID: d.OwnerID, AmountCents: d.AmountCents, Currency: d.Currency, Probability: d.Probability,
		CloseDate: d.CloseDate, NextStepAt: d.NextStepAt, LostReason: d.LostReason, ProjectID: d.ProjectID,
		Overdue: d.Overdue(today), UpdatedAt: d.UpdatedAt,
	}
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
}

func (crmContactView) isJSONResponse() {}

func crmContactViewOf(c model.CRMContact) crmContactView {
	return crmContactView{ID: c.ID, ClientID: c.ClientID, Name: c.Name, Role: c.Role, Email: c.Email, Phone: c.Phone}
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
	ID             int64     `json:"id"`
	Title          string    `json:"title"`
	StartsAt       time.Time `json:"starts_at"`
	EndsAt         time.Time `json:"ends_at"`
	Location       string    `json:"location"`
	Notes          string    `json:"notes"`
	ProjectID      int64     `json:"project_id"`
	ClientID       int64     `json:"client_id"`
	Shared         bool      `json:"shared"`
	ParticipantIDs []int64   `json:"participant_ids"`
	CreatedBy      int64     `json:"created_by"`
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
		ParticipantIDs: participants, CreatedBy: e.CreatedBy,
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
