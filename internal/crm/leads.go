package crm

import (
	"context"
	"net/mail"
	"strings"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// Leads (CRM-14). The rules of who may change a lead live in the persistence transaction;
// this file trims and checks the input, and names the field a refusal is about with the
// JSON name the screens and the agents use.

const maxPhoneLength = 40

// leadFields are a lead's own fields as a person or the website gave them.
type leadFields struct {
	Company, ContactName, Phone, Email, Source, Message, NextStepAt string
}

func trimLead(in leadFields) leadFields {
	return leadFields{
		Company:     strings.TrimSpace(in.Company),
		ContactName: strings.TrimSpace(in.ContactName),
		Phone:       strings.TrimSpace(in.Phone),
		Email:       strings.TrimSpace(in.Email),
		Source:      strings.TrimSpace(in.Source),
		Message:     strings.TrimSpace(in.Message),
		NextStepAt:  strings.TrimSpace(in.NextStepAt),
	}
}

// checkLeadFields refuses a field that is too long, an email that is not a bare address,
// or a date that is not a day. An empty field is fine here; the contact rule is separate.
func checkLeadFields(in leadFields) error {
	switch {
	case len(in.Company) > maxNameLength:
		return fieldError("company")
	case len(in.ContactName) > maxNameLength:
		return fieldError("contact_name")
	case len(in.Phone) > maxPhoneLength:
		return fieldError("phone")
	case len(in.Email) > maxNameLength:
		return fieldError("email")
	case len(in.Source) > maxNameLength:
		return fieldError("source")
	case len(in.Message) > maxNoteLength:
		return fieldError("message")
	}
	if in.Email != "" {
		if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email {
			return fieldError("email")
		}
	}
	if in.NextStepAt != "" && !validDate(in.NextStepAt) {
		return fieldError("next_step_at")
	}
	return nil
}

// reachable refuses a lead nobody can call back or name: it needs a company, a contact,
// a phone or an email.
func reachable(in leadFields) error {
	if in.Company == "" && in.ContactName == "" && in.Phone == "" && in.Email == "" {
		return fieldError("contact")
	}
	return nil
}

// checkNewLead trims and checks a lead that is about to be recorded.
func checkNewLead(in leadFields) (leadFields, error) {
	in = trimLead(in)
	if err := checkLeadFields(in); err != nil {
		return leadFields{}, err
	}
	if err := reachable(in); err != nil {
		return leadFields{}, err
	}
	return in, nil
}

// CreateLead records a lead. The caller is its owner unless a manager names another.
func (s *Service) CreateLead(ctx context.Context, request appmodel.CRMLeadCreateRequest) (model.CRMLead, error) {
	if request.TeamID <= 0 {
		return model.CRMLead{}, appmodel.ErrInvalidWorkspace
	}
	in, err := checkNewLead(leadFields{
		Company: request.Company, ContactName: request.ContactName, Phone: request.Phone, Email: request.Email,
		Source: request.Source, Message: request.Message, NextStepAt: request.NextStepAt,
	})
	if err != nil {
		return model.CRMLead{}, err
	}
	request.Company, request.ContactName, request.Phone, request.Email = in.Company, in.ContactName, in.Phone, in.Email
	request.Source, request.Message, request.NextStepAt = in.Source, in.Message, in.NextStepAt
	return s.store.CreateCRMLead(ctx, request)
}

// Leads lists the workspace's leads, newest first, optionally by status or owner.
func (s *Service) Leads(ctx context.Context, query appmodel.CRMLeadListQuery) ([]model.CRMLead, error) {
	if query.TeamID <= 0 {
		return nil, appmodel.ErrInvalidWorkspace
	}
	return s.store.ListCRMLeads(ctx, query)
}

// UpdateLead changes the fields that are given; a nil field stays as it is. The persistence
// transaction trims the values it writes and checks the contact rule against the lead as it
// is stored.
func (s *Service) UpdateLead(ctx context.Context, request appmodel.CRMLeadUpdateRequest) (model.CRMLead, error) {
	if request.TeamID <= 0 {
		return model.CRMLead{}, appmodel.ErrInvalidWorkspace
	}
	given := trimLead(leadFields{
		Company: deref(request.Company), ContactName: deref(request.ContactName), Phone: deref(request.Phone),
		Email: deref(request.Email), Source: deref(request.Source), Message: deref(request.Message),
		NextStepAt: deref(request.NextStepAt),
	})
	if err := checkLeadFields(given); err != nil {
		return model.CRMLead{}, err
	}
	return s.store.UpdateCRMLead(ctx, request)
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// MoveLeadStatus sets a lead's status. Converted is not a status a person sets: a lead
// becomes converted only through ConvertLead.
func (s *Service) MoveLeadStatus(ctx context.Context, request appmodel.CRMLeadStatusRequest) (model.CRMLead, error) {
	if request.TeamID <= 0 {
		return model.CRMLead{}, appmodel.ErrInvalidWorkspace
	}
	if !model.ValidLeadStatus(request.Status) || request.Status == model.LeadStatusConverted {
		return model.CRMLead{}, fieldError("status")
	}
	request.LostReason = strings.TrimSpace(request.LostReason)
	if len(request.LostReason) > maxNameLength {
		return model.CRMLead{}, fieldError("lost_reason")
	}
	return s.store.MoveCRMLeadStatus(ctx, request)
}

// ConvertLead turns an open lead into a client and a deal. Only a manager may do it, because
// the conversion creates a client.
func (s *Service) ConvertLead(ctx context.Context, request appmodel.CRMLeadConvertRequest) (model.CRMLead, error) {
	if request.TeamID <= 0 {
		return model.CRMLead{}, appmodel.ErrInvalidWorkspace
	}
	return s.store.ConvertCRMLead(ctx, request)
}

// LeadResponseStats measures the first response of the leads created in a period (CRM-16).
// The period is an interval of instants the caller built in the workspace zone; an empty one
// is refused with the field "to".
func (s *Service) LeadResponseStats(ctx context.Context, query appmodel.CRMLeadResponseStatsQuery) (model.CRMLeadResponseStats, error) {
	if query.TeamID <= 0 {
		return model.CRMLeadResponseStats{}, appmodel.ErrInvalidWorkspace
	}
	if !query.To.After(query.From) {
		return model.CRMLeadResponseStats{}, fieldError("to")
	}
	return s.store.CRMLeadResponseStats(ctx, query)
}

// FunnelReport builds the funnel of a period (CRM-17). The period is an interval of instants the
// caller built in the workspace zone, refused with "to" when empty, and Today is the workspace
// day. Any member reads it; the persistence layer keeps the amounts for a manager.
func (s *Service) FunnelReport(ctx context.Context, query appmodel.CRMFunnelQuery) (model.CRMFunnel, error) {
	if query.TeamID <= 0 {
		return model.CRMFunnel{}, appmodel.ErrInvalidWorkspace
	}
	if !query.To.After(query.From) {
		return model.CRMFunnel{}, fieldError("to")
	}
	if !validDate(query.Today) {
		return model.CRMFunnel{}, fieldError("today")
	}
	return s.store.CRMFunnelReport(ctx, query)
}

// CreateIntakeKey makes the workspace's intake key and returns the raw key, which is shown
// once. Only a manager may make one.
func (s *Service) CreateIntakeKey(ctx context.Context, request appmodel.CRMIntakeKeyRequest) (string, model.CRMIntakeKey, error) {
	if request.TeamID <= 0 {
		return "", model.CRMIntakeKey{}, appmodel.ErrInvalidWorkspace
	}
	if !request.Manager {
		return "", model.CRMIntakeKey{}, model.ErrForbidden
	}
	return s.store.CreateCRMIntakeKey(ctx, request)
}

// RevokeIntakeKey removes the workspace's intake key. Only a manager may revoke it.
func (s *Service) RevokeIntakeKey(ctx context.Context, request appmodel.CRMIntakeKeyRequest) error {
	if request.TeamID <= 0 {
		return appmodel.ErrInvalidWorkspace
	}
	if !request.Manager {
		return model.ErrForbidden
	}
	return s.store.RevokeCRMIntakeKey(ctx, request)
}

// IntakeKeyStatus reads whether the workspace has an intake key, and when it was made.
func (s *Service) IntakeKeyStatus(ctx context.Context, query appmodel.CRMIntakeKeyQuery) (model.CRMIntakeKey, error) {
	if query.TeamID <= 0 {
		return model.CRMIntakeKey{}, appmodel.ErrInvalidWorkspace
	}
	return s.store.CRMIntakeKeyStatus(ctx, query)
}

// IntakeKeyTeam returns the workspace a key belongs to. It runs before the enquiry body is
// read, so a request with a wrong key gets the same answer whatever it sends.
func (s *Service) IntakeKeyTeam(ctx context.Context, key string) (int64, error) {
	return s.store.IntakeKeyTeam(ctx, key)
}

// CaptureIntakeLead records an enquiry from the website for the workspace its key named.
// It is checked like a lead a person records, so the website cannot store less than a
// person would have to give.
func (s *Service) CaptureIntakeLead(ctx context.Context, request appmodel.CRMLeadIntakeRequest) (model.CRMLead, error) {
	if request.TeamID <= 0 {
		return model.CRMLead{}, appmodel.ErrInvalidWorkspace
	}
	in, err := checkNewLead(leadFields{
		Company: request.Company, ContactName: request.ContactName, Phone: request.Phone, Email: request.Email,
		Source: request.Source, Message: request.Message,
	})
	if err != nil {
		return model.CRMLead{}, err
	}
	request.Company, request.ContactName, request.Phone, request.Email = in.Company, in.ContactName, in.Phone, in.Email
	request.Source, request.Message = in.Source, in.Message
	return s.store.CreateIntakeLead(ctx, request)
}
