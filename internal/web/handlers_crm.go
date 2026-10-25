package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/i18n"
	"github.com/aa-blinov/paratrack/internal/model"
)

// maxCRMBody bounds one JSON write. A deal or an event is a few hundred bytes, so
// a larger body is a mistake or an abuse.
const maxCRMBody = 64 << 10

type crmPageData struct {
	pageData
	CRMReact bool
}

// calendarPageData hands the screen the workspace zone and its wall clock, so the
// week, the day and the default start of a new event come from the same zone the
// server reads the times in, not from the browser's.
type calendarPageData struct {
	pageData
	CalendarReact bool
	Zone          string
	Today         string
	NowLocal      string
	// WeekStartsSunday is the workspace's first day of the week, or the person's own
	// when the workspace leaves it open; the timesheet and the schedule read it too.
	WeekStartsSunday bool
}

func (s *Server) handleCRMPage(w http.ResponseWriter, r *http.Request) {
	data := crmPageData{pageData: pageData{Title: "CRM", Active: "crm", Lang: string(resolveLang(r))}}
	data.ReactApp = true
	data.CRMReact = true
	s.renderPageForRequest(w, r, data.Title, data.Active, &data)
}

func (s *Server) handleCalendarPage(w http.ResponseWriter, r *http.Request) {
	now := userNow(r)
	data := calendarPageData{
		pageData: pageData{Title: "Calendar", Active: "calendar", Lang: string(resolveLang(r))},
		Zone:     userLoc(r).String(), Today: now.Format(time.DateOnly), NowLocal: now.Format(calendarWallClock),
		WeekStartsSunday: weekStartsSunday(r),
	}
	data.ReactApp = true
	data.CalendarReact = true
	s.renderPageForRequest(w, r, data.Title, data.Active, &data)
}

// apiFieldErrorResponse names the field the caller has to fix. The React forms use
// the field to mark the input; the MCP tools carry the same text to the agent.
type apiFieldErrorResponse struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

func (apiFieldErrorResponse) isJSONResponse() {}

// writeCRMError maps the CRM and calendar workflow errors to HTTP. Input problems
// are 400 with the field, rule breaks are 403, and a record outside the caller's
// workspace is 404.
func (s *Server) writeCRMError(w http.ResponseWriter, r *http.Request, err error) {
	lang := resolveLang(r)
	if field, ok := crmInputField(err); ok {
		s.writeJSONStatus(w, http.StatusBadRequest, apiFieldErrorResponse{Error: i18n.T(lang, "err.invalidInput"), Field: field})
		return
	}
	switch {
	case errors.Is(err, model.ErrForbidden):
		s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: i18n.T(lang, "err.forbiddenAction")})
	case errors.Is(err, model.ErrProjectManagerRequired):
		s.writeJSONStatus(w, http.StatusForbidden, apiErrorResponse{Error: i18n.T(lang, "crm.projectManagerOnly")})
	case errors.Is(err, model.ErrNotFound):
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: i18n.T(lang, "err.notFoundEntity")})
	default:
		s.writeInternalJSONError(w, err)
	}
}

// crmInputField names the input a workflow refused, or reports false when the error
// is not about input. The HTTP routes and the MCP tools both read it from here.
func crmInputField(err error) (string, bool) {
	var field appmodel.FieldError
	switch {
	case errors.As(err, &field):
		return field.Field, true
	case errors.Is(err, model.ErrInvalidDealStage):
		return "stage", true
	case errors.Is(err, model.ErrLostReasonRequired):
		return "lost_reason", true
	case errors.Is(err, model.ErrInvalidActivityKind):
		return "kind", true
	case errors.Is(err, model.ErrEventEndBeforeStart):
		return "ends_at", true
	case errors.Is(err, model.ErrCalendarTargetOutOfTeam):
		return "links", true
	}
	return "", false
}

// decodeJSONBody reads one JSON object from the request. It writes the 400 itself
// and reports false when the body is not a valid object.
func (s *Server) decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCRMBody))
	if err == nil {
		err = json.Unmarshal(body, dst)
	}
	if err != nil {
		s.writeJSONStatus(w, http.StatusBadRequest, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.invalidInput")})
		return false
	}
	return true
}

// crmPathID reads a positive id from the route, writing the 404 itself on failure.
func (s *Server) crmPathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		s.writeJSONStatus(w, http.StatusNotFound, apiErrorResponse{Error: i18n.T(resolveLang(r), "err.notFoundEntity")})
		return 0, false
	}
	return id, true
}

// crmQueryID reads an optional id from the query string. A missing or malformed
// value is zero, which the workflows read as "not set".
func crmQueryID(r *http.Request, key string) int64 {
	id, _ := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	return id
}

// workspaceToday is the caller's day in YYYY-MM-DD, the frame for overdue checks.
func workspaceToday(r *http.Request) string {
	return userNow(r).Format(time.DateOnly)
}

type crmDealBody struct {
	Title       string `json:"title"`
	Stage       string `json:"stage"`
	ClientID    int64  `json:"client_id"`
	ContactID   int64  `json:"contact_id"`
	OwnerID     int64  `json:"owner_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	Probability int    `json:"probability"`
	CloseDate   string `json:"close_date"`
	NextStepAt  string `json:"next_step_at"`
}

// crmDealUpdateBody changes only the fields it names. A null or missing field is
// left as it is.
type crmDealUpdateBody struct {
	Title       *string `json:"title"`
	OwnerID     *int64  `json:"owner_id"`
	ClientID    *int64  `json:"client_id"`
	ContactID   *int64  `json:"contact_id"`
	AmountCents *int64  `json:"amount_cents"`
	Probability *int    `json:"probability"`
	CloseDate   *string `json:"close_date"`
	NextStepAt  *string `json:"next_step_at"`
}

type crmStageBody struct {
	Stage      string `json:"stage"`
	LostReason string `json:"lost_reason"`
}

type crmStageCreateBody struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// crmStageUpdateBody changes only the fields it names.
type crmStageUpdateBody struct {
	Name     *string `json:"name"`
	Kind     *string `json:"kind"`
	Position *int    `json:"position"`
}

type crmContactBody struct {
	ClientID int64  `json:"client_id"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
}

type crmActivityBody struct {
	ClientID   int64     `json:"client_id"`
	DealID     int64     `json:"deal_id"`
	Kind       string    `json:"kind"`
	Body       string    `json:"body"`
	Result     string    `json:"result"`
	OccurredAt time.Time `json:"occurred_at"`
}

// calendarWallClock is the form the date-time field reads and writes.
const calendarWallClock = "2006-01-02T15:04"

type calendarEventBody struct {
	Title          string  `json:"title"`
	StartsAt       string  `json:"starts_at"`
	EndsAt         string  `json:"ends_at"`
	Location       string  `json:"location"`
	Notes          string  `json:"notes"`
	ProjectID      int64   `json:"project_id"`
	ClientID       int64   `json:"client_id"`
	Shared         bool    `json:"shared"`
	ParticipantIDs []int64 `json:"participant_ids"`
}

type calendarEventUpdateBody struct {
	Title          *string `json:"title"`
	StartsAt       *string `json:"starts_at"`
	EndsAt         *string `json:"ends_at"`
	Location       *string `json:"location"`
	Notes          *string `json:"notes"`
	ProjectID      *int64  `json:"project_id"`
	ClientID       *int64  `json:"client_id"`
	Shared         *bool   `json:"shared"`
	ParticipantIDs []int64 `json:"participant_ids"`
}

// calendarMoment reads a moment the way the calendar writes it: a wall clock in the
// workspace zone, or RFC 3339 with its own offset. An empty value stays the zero
// time, which the workflow then refuses by name.
func calendarMoment(field, value string, loc *time.Location) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	if moment, err := time.Parse(time.RFC3339, value); err == nil {
		return moment, nil
	}
	moment, err := time.ParseInLocation(calendarWallClock, value, loc)
	if err != nil {
		return time.Time{}, appmodel.FieldError{Field: field}
	}
	return moment, nil
}

// optionalCalendarMoment reads a moment only when the update names it.
func optionalCalendarMoment(field string, value *string, loc *time.Location) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	moment, err := calendarMoment(field, *value, loc)
	if err != nil {
		return nil, err
	}
	return &moment, nil
}

func (s *Server) handleCRMDealsList(w http.ResponseWriter, r *http.Request) {
	today := workspaceToday(r)
	base := appmodel.CRMDealListQuery{TeamID: teamID(r), CallerID: authenticatedUserID(r), Today: today}
	query := base
	query.Stage = r.URL.Query().Get("stage")
	query.OwnerID = crmQueryID(r, "owner")
	query.Overdue = r.URL.Query().Get("overdue") == "1"
	deals, err := s.services.CRM.Deals(r.Context(), query)
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	pipeline, err := s.services.CRM.Pipeline(r.Context(), base)
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	views := make([]crmDealView, 0, len(deals))
	for _, d := range deals {
		views = append(views, crmDealViewOf(d, today))
	}
	s.writeJSON(w, crmDealsResponse{Today: today, Deals: views, Pipeline: crmPipelineViewOf(pipeline, resolveLang(r))})
}

func crmPipelineViewOf(p appmodel.CRMPipeline, lang i18n.Lang) crmPipelineView {
	out := crmPipelineView{Stages: make([]crmStageView, 0, len(p.Stages)), Overdue: p.Overdue}
	for _, stage := range p.Stages {
		out.Stages = append(out.Stages, crmStageView{Stage: stage.Stage, Name: stageName(lang, stage.Stage, stage.Name), Kind: stage.Kind, Count: stage.Count})
	}
	return out
}

func (s *Server) handleCRMStagesList(w http.ResponseWriter, r *http.Request) {
	stages, err := s.services.CRM.Stages(r.Context(), appmodel.CRMStageListQuery{TeamID: teamID(r), CallerID: authenticatedUserID(r)})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmStagesResponse{Stages: crmStageConfigViews(stages, resolveLang(r))})
}

func (s *Server) handleCRMStageCreate(w http.ResponseWriter, r *http.Request) {
	var body crmStageCreateBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	stage, err := s.services.CRM.CreateStage(r.Context(), appmodel.CRMStageCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Manager: canManage(r), Name: body.Name, Kind: body.Kind,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmStageConfigViewOf(stage, resolveLang(r)))
}

// handleCRMStageUpdate renames or re-kinds a stage, or moves it when the body only
// gives a position. The key addresses the stage, because a default stage has no row
// until the first change and so no id.
func (s *Server) handleCRMStageUpdate(w http.ResponseWriter, r *http.Request) {
	var body crmStageUpdateBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	var stage model.CRMStage
	var err error
	if body.Position != nil && body.Name == nil && body.Kind == nil {
		stage, err = s.services.CRM.MoveStage(r.Context(), appmodel.CRMStageMoveRequest{
			TeamID: teamID(r), CallerID: authenticatedUserID(r), Manager: canManage(r), Key: r.PathValue("key"), Position: *body.Position,
		})
	} else {
		stage, err = s.services.CRM.UpdateStage(r.Context(), appmodel.CRMStageUpdateRequest{
			TeamID: teamID(r), CallerID: authenticatedUserID(r), Manager: canManage(r), Key: r.PathValue("key"),
			Name: body.Name, Kind: body.Kind, Position: body.Position,
		})
	}
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmStageConfigViewOf(stage, resolveLang(r)))
}

func (s *Server) handleCRMStageDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.services.CRM.DeleteStage(r.Context(), appmodel.CRMStageDeleteRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Manager: canManage(r), Key: r.PathValue("key"),
	}); err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleCRMDealCreate(w http.ResponseWriter, r *http.Request) {
	var body crmDealBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	deal, err := s.services.CRM.CreateDeal(r.Context(), appmodel.CRMDealCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Title: body.Title, Stage: body.Stage,
		ClientID: body.ClientID, ContactID: body.ContactID, OwnerID: body.OwnerID, AmountCents: body.AmountCents,
		Currency: body.Currency, Probability: body.Probability, CloseDate: body.CloseDate, NextStepAt: body.NextStepAt,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmDealViewOf(deal, workspaceToday(r)))
}

func (s *Server) handleCRMDealUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.crmPathID(w, r, "id")
	if !ok {
		return
	}
	var body crmDealUpdateBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	deal, err := s.services.CRM.UpdateDeal(r.Context(), appmodel.CRMDealUpdateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), DealID: id, Title: body.Title, OwnerID: body.OwnerID,
		ClientID: body.ClientID, ContactID: body.ContactID, AmountCents: body.AmountCents,
		Probability: body.Probability, CloseDate: body.CloseDate, NextStepAt: body.NextStepAt,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmDealViewOf(deal, workspaceToday(r)))
}

func (s *Server) handleCRMDealStage(w http.ResponseWriter, r *http.Request) {
	id, ok := s.crmPathID(w, r, "id")
	if !ok {
		return
	}
	var body crmStageBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	deal, err := s.services.CRM.MoveDealStage(r.Context(), appmodel.CRMDealStageRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), DealID: id, Stage: body.Stage, LostReason: body.LostReason,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmDealViewOf(deal, workspaceToday(r)))
}

// handleCRMDealProject makes the project of a won deal. The deal rules and the project
// rule both decide in the workflow, and a refusal names its field or its reason.
func (s *Server) handleCRMDealProject(w http.ResponseWriter, r *http.Request) {
	id, ok := s.crmPathID(w, r, "id")
	if !ok {
		return
	}
	deal, err := s.services.CRM.CreateDealProject(r.Context(), appmodel.CRMDealProjectRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), DealID: id,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmDealViewOf(deal, workspaceToday(r)))
}

func (s *Server) handleCRMContactsList(w http.ResponseWriter, r *http.Request) {
	contacts, err := s.services.CRM.Contacts(r.Context(), appmodel.CRMContactListQuery{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), ClientID: crmQueryID(r, "client_id"),
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	out := make([]crmContactView, 0, len(contacts))
	for _, c := range contacts {
		out = append(out, crmContactViewOf(c))
	}
	s.writeJSON(w, crmContactsResponse{Contacts: out})
}

func (s *Server) handleCRMContactCreate(w http.ResponseWriter, r *http.Request) {
	var body crmContactBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	contact, err := s.services.CRM.CreateContact(r.Context(), appmodel.CRMContactCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), ClientID: body.ClientID, Name: body.Name,
		Role: body.Role, Email: body.Email, Phone: body.Phone,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmContactViewOf(contact))
}

func (s *Server) handleCRMActivitiesList(w http.ResponseWriter, r *http.Request) {
	activities, err := s.services.CRM.Activities(r.Context(), appmodel.CRMActivityListQuery{
		TeamID: teamID(r), CallerID: authenticatedUserID(r),
		ClientID: crmQueryID(r, "client_id"), DealID: crmQueryID(r, "deal_id"),
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	loc := userLoc(r)
	out := make([]crmActivityView, 0, len(activities))
	for _, a := range activities {
		out = append(out, crmActivityViewIn(a, loc))
	}
	s.writeJSON(w, crmActivitiesResponse{Activities: out})
}

func (s *Server) handleCRMActivityCreate(w http.ResponseWriter, r *http.Request) {
	var body crmActivityBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	activity, err := s.services.CRM.LogActivity(r.Context(), appmodel.CRMActivityCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), ClientID: body.ClientID, DealID: body.DealID,
		Kind: body.Kind, Body: body.Body, Result: body.Result, OccurredAt: body.OccurredAt,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmActivityViewIn(activity, userLoc(r)))
}

// handleCalendarEventsList answers for the days from and to, both inclusive, in the
// workspace zone. An event that crosses midnight shows on each day it touches.
func (s *Server) handleCalendarEventsList(w http.ResponseWriter, r *http.Request) {
	loc := userLoc(r)
	first, errFirst := time.ParseInLocation(time.DateOnly, r.URL.Query().Get("from"), loc)
	last, errLast := time.ParseInLocation(time.DateOnly, r.URL.Query().Get("to"), loc)
	if errFirst != nil || errLast != nil {
		s.writeCRMError(w, r, appmodel.FieldError{Field: "from and to"})
		return
	}
	events, err := s.services.Calendar.Agenda(r.Context(), appmodel.CalendarEventListQuery{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), From: first, To: last.AddDate(0, 0, 1),
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, calendarEventsResponse{Events: calendarEventViewsIn(events, loc)})
}

func (s *Server) handleCalendarEventCreate(w http.ResponseWriter, r *http.Request) {
	var body calendarEventBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	loc := userLoc(r)
	startsAt, err := calendarMoment("starts_at", body.StartsAt, loc)
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	endsAt, err := calendarMoment("ends_at", body.EndsAt, loc)
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	event, err := s.services.Calendar.Create(r.Context(), appmodel.CalendarEventCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Title: body.Title, StartsAt: startsAt,
		EndsAt: endsAt, Location: body.Location, Notes: body.Notes, ProjectID: body.ProjectID,
		ClientID: body.ClientID, Shared: body.Shared, ParticipantIDs: body.ParticipantIDs,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, calendarEventViewIn(event, loc))
}

func (s *Server) handleCalendarEventUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.crmPathID(w, r, "id")
	if !ok {
		return
	}
	var body calendarEventUpdateBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	loc := userLoc(r)
	startsAt, err := optionalCalendarMoment("starts_at", body.StartsAt, loc)
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	endsAt, err := optionalCalendarMoment("ends_at", body.EndsAt, loc)
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	event, err := s.services.Calendar.Update(r.Context(), appmodel.CalendarEventUpdateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), EventID: id, Title: body.Title,
		StartsAt: startsAt, EndsAt: endsAt, Location: body.Location, Notes: body.Notes,
		ProjectID: body.ProjectID, ClientID: body.ClientID, Shared: body.Shared, ParticipantIDs: body.ParticipantIDs,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, calendarEventViewIn(event, loc))
}

func (s *Server) handleCalendarEventDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := s.crmPathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.services.Calendar.Delete(r.Context(), appmodel.CalendarEventDeleteRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), EventID: id,
	}); err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
