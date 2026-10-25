package web

import (
	"errors"
	"net/http"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// leadIntakeKeyHeader carries the workspace's intake key. It is the only credential the
// enquiry route reads: a session cookie or a Bearer token sent there is ignored.
const leadIntakeKeyHeader = "X-Intake-Key"

// crmLeadBody is a lead a person records. The website's enquiry has a narrower body.
type crmLeadBody struct {
	Company     string `json:"company"`
	ContactName string `json:"contact_name"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Source      string `json:"source"`
	Message     string `json:"message"`
	OwnerID     int64  `json:"owner_id"`
	NextStepAt  string `json:"next_step_at"`
}

// crmLeadUpdateBody changes only the fields it names.
type crmLeadUpdateBody struct {
	Company     *string `json:"company"`
	ContactName *string `json:"contact_name"`
	Phone       *string `json:"phone"`
	Email       *string `json:"email"`
	Source      *string `json:"source"`
	Message     *string `json:"message"`
	OwnerID     *int64  `json:"owner_id"`
	NextStepAt  *string `json:"next_step_at"`
}

type crmLeadStatusBody struct {
	Status     string `json:"status"`
	LostReason string `json:"lost_reason"`
}

// leadIntakeBody is what the company website sends. It cannot name an owner or a status.
type leadIntakeBody struct {
	Company     string `json:"company"`
	ContactName string `json:"contact_name"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Source      string `json:"source"`
	Message     string `json:"message"`
}

// leadIntakeResponse is the answer to an accepted enquiry: the lead's id and nothing else.
type leadIntakeResponse struct {
	ID int64 `json:"id"`
}

func (leadIntakeResponse) isJSONResponse() {}

func (s *Server) handleCRMLeadsList(w http.ResponseWriter, r *http.Request) {
	leads, err := s.services.CRM.Leads(r.Context(), appmodel.CRMLeadListQuery{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Status: r.URL.Query().Get("status"), OwnerID: crmQueryID(r, "owner"),
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmLeadsResponse{Leads: crmLeadViews(leads, userNow(r))})
}

// leadPeriod reads the first and last day of a lead period in the reader's zone, and returns
// the instants the workspace query takes: the start of the first day and the start of the day
// after the last one. The same reading serves the HTTP route and the MCP tool.
func leadPeriod(from, to string, loc *time.Location) (time.Time, time.Time, error) {
	start, err := mcpLocalDate("from", from, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	last, err := mcpLocalDate("to", to, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, last.AddDate(0, 0, 1), nil
}

// handleCRMLeadResponseStats measures the first response of the leads created from one day to
// another (CRM-16). Any member reads it, as the leads list does.
func (s *Server) handleCRMLeadResponseStats(w http.ResponseWriter, r *http.Request) {
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	start, end, err := leadPeriod(from, to, userLoc(r))
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	stats, err := s.services.CRM.LeadResponseStats(r.Context(), appmodel.CRMLeadResponseStatsQuery{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), From: start, To: end,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmLeadResponseStatsViewOf(stats, from, to))
}

// handleCRMFunnelReport is the funnel of a period (CRM-17). The period is read in the reader's
// workspace zone, as the response stats are, and overdue steps are counted against today there.
// Any member reads it; the workflow decides which totals carry amounts.
func (s *Server) handleCRMFunnelReport(w http.ResponseWriter, r *http.Request) {
	from, to := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	start, end, err := leadPeriod(from, to, userLoc(r))
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	today := workspaceToday(r)
	report, err := s.services.CRM.FunnelReport(r.Context(), appmodel.CRMFunnelQuery{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), From: start, To: end, Today: today,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmFunnelViewOf(report, resolveLang(r), from, to, today))
}

func (s *Server) handleCRMLeadCreate(w http.ResponseWriter, r *http.Request) {
	var body crmLeadBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	lead, err := s.services.CRM.CreateLead(r.Context(), appmodel.CRMLeadCreateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), OwnerID: body.OwnerID, Company: body.Company,
		ContactName: body.ContactName, Phone: body.Phone, Email: body.Email, Source: body.Source,
		Message: body.Message, NextStepAt: body.NextStepAt,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmLeadViewOf(lead, userNow(r)))
}

func (s *Server) handleCRMLeadUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := s.crmPathID(w, r, "id")
	if !ok {
		return
	}
	var body crmLeadUpdateBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	lead, err := s.services.CRM.UpdateLead(r.Context(), appmodel.CRMLeadUpdateRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), LeadID: id, OwnerID: body.OwnerID,
		Company: body.Company, ContactName: body.ContactName, Phone: body.Phone, Email: body.Email,
		Source: body.Source, Message: body.Message, NextStepAt: body.NextStepAt,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmLeadViewOf(lead, userNow(r)))
}

func (s *Server) handleCRMLeadStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := s.crmPathID(w, r, "id")
	if !ok {
		return
	}
	var body crmLeadStatusBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	lead, err := s.services.CRM.MoveLeadStatus(r.Context(), appmodel.CRMLeadStatusRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), LeadID: id, Status: body.Status, LostReason: body.LostReason,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmLeadViewOf(lead, userNow(r)))
}

func (s *Server) handleCRMLeadConvert(w http.ResponseWriter, r *http.Request) {
	id, ok := s.crmPathID(w, r, "id")
	if !ok {
		return
	}
	lead, err := s.services.CRM.ConvertLead(r.Context(), appmodel.CRMLeadConvertRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), LeadID: id,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, crmLeadViewOf(lead, userNow(r)))
}

func (s *Server) handleCRMIntakeKeyStatus(w http.ResponseWriter, r *http.Request) {
	key, err := s.services.CRM.IntakeKeyStatus(r.Context(), appmodel.CRMIntakeKeyQuery{TeamID: teamID(r), CallerID: authenticatedUserID(r)})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSON(w, intakeKeyResponse(key, ""))
}

// handleCRMIntakeKeyCreate makes the key and returns it in this answer only. A new key
// replaces the old one, which stops working at once.
func (s *Server) handleCRMIntakeKeyCreate(w http.ResponseWriter, r *http.Request) {
	raw, key, err := s.services.CRM.CreateIntakeKey(r.Context(), appmodel.CRMIntakeKeyRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Manager: canManage(r),
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.audit(r, "crm.intake_key_create", "", "")
	w.Header().Set("Cache-Control", "no-store")
	s.writeJSON(w, intakeKeyResponse(key, raw))
}

func (s *Server) handleCRMIntakeKeyRevoke(w http.ResponseWriter, r *http.Request) {
	if err := s.services.CRM.RevokeIntakeKey(r.Context(), appmodel.CRMIntakeKeyRequest{
		TeamID: teamID(r), CallerID: authenticatedUserID(r), Manager: canManage(r),
	}); err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.audit(r, "crm.intake_key_revoke", "", "")
	s.writeJSON(w, crmIntakeKeyResponse{Active: false})
}

// intakeKeyResponse shapes the key state. A zero CreatedAt means the workspace has no key.
func intakeKeyResponse(key model.CRMIntakeKey, raw string) crmIntakeKeyResponse {
	if key.CreatedAt.IsZero() {
		return crmIntakeKeyResponse{Active: false}
	}
	created := key.CreatedAt
	return crmIntakeKeyResponse{Active: true, CreatedAt: &created, Key: raw}
}

// leadIntakeHandler is the enquiry route: a budget per address and a budget per key, then
// the handler. The budgets are checked before the key, so a flood of wrong keys is cut off
// by its address.
func (s *Server) leadIntakeHandler() http.Handler {
	byAddress := s.rateLimitKeyed("intake-ip", 60, time.Minute, clientIP)
	byKey := s.rateLimitKeyed("intake-key", 30, time.Minute, func(r *http.Request) string {
		return r.Header.Get(leadIntakeKeyHeader)
	})
	return leadIntakeCORS(byAddress(byKey(http.HandlerFunc(s.handleLeadIntake))))
}

// handleLeadIntake takes an enquiry from the company website. The key is checked before the
// body is read, so a request without a valid key learns nothing about the fields. The answer
// to an accepted enquiry is the new lead's id.
func (s *Server) handleLeadIntake(w http.ResponseWriter, r *http.Request) {
	workspace, err := s.services.CRM.IntakeKeyTeam(r.Context(), r.Header.Get(leadIntakeKeyHeader))
	if err != nil {
		if errors.Is(err, model.ErrIntakeKeyInvalid) {
			apiUnauthorized(w, r)
			return
		}
		s.writeCRMError(w, r, err)
		return
	}
	var body leadIntakeBody
	if !s.decodeJSONBody(w, r, &body) {
		return
	}
	lead, err := s.services.CRM.CaptureIntakeLead(r.Context(), appmodel.CRMLeadIntakeRequest{
		TeamID: workspace, Company: body.Company, ContactName: body.ContactName, Phone: body.Phone,
		Email: body.Email, Source: body.Source, Message: body.Message,
	})
	if err != nil {
		s.writeCRMError(w, r, err)
		return
	}
	s.writeJSONStatus(w, http.StatusCreated, leadIntakeResponse{ID: lead.ID})
}

// leadIntakeCORS lets any page read the answer to an enquiry. Only POST is allowed, with
// the JSON type and the intake key header, and no credentials: the browser sends no cookie
// with it, so a visitor's session is never part of the request.
func leadIntakeCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, "+leadIntakeKeyHeader)
		h.Set("Access-Control-Max-Age", "600")
		next.ServeHTTP(w, r)
	})
}

func leadIntakePreflight(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
