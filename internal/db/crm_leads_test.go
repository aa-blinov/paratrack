package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A member records a lead and moves it; a lost lead needs a reason, and leaving lost clears
// it; another member cannot move the lead; a converted status is only set by converting.
func TestCRMLeadStatusRules(t *testing.T) {
	d := openTestDB(t)
	d.now = func() time.Time { return time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC) }
	ws := seedRichWorkspace(t, d, "crm-lead-status")

	lead, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, Company: "Лотос", Email: "lotos@lotos.test", Source: "website",
	})
	if err != nil {
		t.Fatalf("member records a lead: %v", err)
	}
	if lead.OwnerID != ws.memberID || lead.Status != model.LeadStatusNew {
		t.Fatalf("new lead = %+v, want the member as owner and status new", lead)
	}

	if _, err := d.MoveCRMLeadStatus(t.Context(), appmodel.CRMLeadStatusRequest{TeamID: ws.teamID, CallerID: ws.memberID, LeadID: lead.ID, Status: model.LeadStatusLost}); !errors.Is(err, model.ErrLostReasonRequired) {
		t.Fatalf("lost without a reason = %v, want ErrLostReasonRequired", err)
	}
	lost, err := d.MoveCRMLeadStatus(t.Context(), appmodel.CRMLeadStatusRequest{TeamID: ws.teamID, CallerID: ws.memberID, LeadID: lead.ID, Status: model.LeadStatusLost, LostReason: "нет бюджета"})
	if err != nil {
		t.Fatalf("lost with a reason: %v", err)
	}
	if lost.Status != model.LeadStatusLost || lost.LostReason != "нет бюджета" {
		t.Fatalf("after lost = %+v", lost)
	}
	reopened, err := d.MoveCRMLeadStatus(t.Context(), appmodel.CRMLeadStatusRequest{TeamID: ws.teamID, CallerID: ws.memberID, LeadID: lead.ID, Status: model.LeadStatusContacted})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.LostReason != "" || reopened.Status != model.LeadStatusContacted {
		t.Fatalf("a reopened lead keeps its reason: %+v", reopened)
	}

	if _, err := d.MoveCRMLeadStatus(t.Context(), appmodel.CRMLeadStatusRequest{TeamID: ws.teamID, CallerID: ws.memberID, LeadID: lead.ID, Status: model.LeadStatusConverted}); !isFieldError(err, "status") {
		t.Fatalf("status converted set by hand = %v, want field status", err)
	}

	// The owner is the member; the manager may move it, another member may not.
	other := seedApprovalUser(t, d, "crm-lead-status-other@test.local")
	if _, err := d.TestSQL().ExecContext(t.Context(), `INSERT INTO memberships (team_id, user_id, role, joined_at, hourly_pay_cents, capacity_minutes, admin_minutes) VALUES (?, ?, 'member', ?, 0, 2400, 60)`, ws.teamID, other, FormatTime(time.Now().UTC())); err != nil {
		t.Fatalf("second member: %v", err)
	}
	if _, err := d.MoveCRMLeadStatus(t.Context(), appmodel.CRMLeadStatusRequest{TeamID: ws.teamID, CallerID: other, LeadID: lead.ID, Status: model.LeadStatusQualified}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member who does not own the lead moved it: %v", err)
	}
	if _, err := d.MoveCRMLeadStatus(t.Context(), appmodel.CRMLeadStatusRequest{TeamID: ws.teamID, CallerID: ws.ownerID, LeadID: lead.ID, Status: model.LeadStatusQualified}); err != nil {
		t.Fatalf("manager moves a lead: %v", err)
	}
}

// Converting makes a client with the lead's email, a contact and a deal in the first open
// stage with the lead's next step; the links are kept, and a second conversion is refused.
// A lost lead is not converted at all.
func TestCRMLeadConvertMakesClientAndDeal(t *testing.T) {
	d := openTestDB(t)
	d.now = func() time.Time { return time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC) }
	ws := seedRichWorkspace(t, d, "crm-lead-convert")

	lead, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, Company: "Лотос", ContactName: "Анна", Phone: "+7 900 000-00-00",
		Email: "anna@lotos.test", Message: "Нужен сайт", NextStepAt: "2026-11-01",
	})
	if err != nil {
		t.Fatalf("record lead: %v", err)
	}
	if _, err := d.ConvertCRMLead(t.Context(), appmodel.CRMLeadConvertRequest{TeamID: ws.teamID, CallerID: ws.memberID, LeadID: lead.ID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member converted a lead = %v, want ErrForbidden", err)
	}
	converted, err := d.ConvertCRMLead(t.Context(), appmodel.CRMLeadConvertRequest{TeamID: ws.teamID, CallerID: ws.ownerID, LeadID: lead.ID})
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if converted.Status != model.LeadStatusConverted || converted.ConvertedClientID == 0 || converted.ConvertedDealID == 0 {
		t.Fatalf("converted lead = %+v, want the status and both links", converted)
	}

	sql := d.TestSQL()
	var clientName, clientEmail string
	if err := sql.QueryRowContext(t.Context(), `SELECT name, email FROM clients WHERE id = ? AND team_id = ?`, converted.ConvertedClientID, ws.teamID).Scan(&clientName, &clientEmail); err != nil {
		t.Fatalf("client row: %v", err)
	}
	if clientName != "Лотос" || clientEmail != "anna@lotos.test" {
		t.Fatalf("client = %q %q, want the company and the lead's email", clientName, clientEmail)
	}

	deal, err := d.GetCRMDeal(t.Context(), appmodel.CRMDealLookupQuery{TeamID: ws.teamID, CallerID: ws.memberID, DealID: converted.ConvertedDealID})
	if err != nil {
		t.Fatalf("deal: %v", err)
	}
	if deal.ClientID != converted.ConvertedClientID || deal.Stage != model.DealStageNew || deal.OwnerID != ws.memberID || deal.NextStepAt != "2026-11-01" {
		t.Fatalf("deal = %+v, want the client, the first open stage, the owner and the next step", deal)
	}
	if deal.ContactID == 0 {
		t.Fatal("the deal has no contact although the lead named one")
	}

	if _, err := d.ConvertCRMLead(t.Context(), appmodel.CRMLeadConvertRequest{TeamID: ws.teamID, CallerID: ws.ownerID, LeadID: lead.ID}); !isFieldError(err, "status") {
		t.Fatalf("a second conversion = %v, want field status", err)
	}

	// The lead's message is the deal's first note, and the lead's activities stay on the lead.
	if _, err := d.CreateCRMActivity(t.Context(), appmodel.CRMActivityCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, LeadID: lead.ID, Kind: "call", Body: "Перезвонила"}); err != nil {
		t.Fatalf("log on the converted lead: %v", err)
	}
	notes, err := d.ListCRMActivities(t.Context(), appmodel.CRMActivityListQuery{TeamID: ws.teamID, CallerID: ws.memberID, DealID: deal.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Body != "Нужен сайт" {
		t.Fatalf("deal timeline = %+v, want the lead's message", notes)
	}
	onLead, err := d.ListCRMActivities(t.Context(), appmodel.CRMActivityListQuery{TeamID: ws.teamID, CallerID: ws.memberID, LeadID: lead.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(onLead) != 1 || onLead[0].LeadID != lead.ID {
		t.Fatalf("lead timeline = %+v, want the call", onLead)
	}

	lost, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Company: "Одуванчик"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MoveCRMLeadStatus(t.Context(), appmodel.CRMLeadStatusRequest{TeamID: ws.teamID, CallerID: ws.memberID, LeadID: lost.ID, Status: model.LeadStatusLost, LostReason: "нет"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ConvertCRMLead(t.Context(), appmodel.CRMLeadConvertRequest{TeamID: ws.teamID, CallerID: ws.ownerID, LeadID: lost.ID}); !isFieldError(err, "status") {
		t.Fatalf("a lost lead converted: %v", err)
	}
}

// A client name the workspace already has refuses the conversion and writes nothing: the
// lead stays open and no deal appears.
func TestCRMLeadConvertNameTakenWritesNothing(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-lead-taken")

	lead, err := d.CreateCRMLead(t.Context(), appmodel.CRMLeadCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Company: "Ромашка"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ConvertCRMLead(t.Context(), appmodel.CRMLeadConvertRequest{TeamID: ws.teamID, CallerID: ws.ownerID, LeadID: lead.ID}); !isFieldError(err, "client name") {
		t.Fatalf("a taken client name = %v, want field client name", err)
	}
	open, err := d.ListCRMLeads(t.Context(), appmodel.CRMLeadListQuery{TeamID: ws.teamID, CallerID: ws.ownerID, Status: model.LeadStatusNew})
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].ID != lead.ID || open[0].ConvertedDealID != 0 {
		t.Fatalf("the refused conversion changed the lead: %+v", open)
	}
	var deals int
	if err := d.TestSQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM crm_deals WHERE team_id = ? AND title = 'Ромашка'`, ws.teamID).Scan(&deals); err != nil {
		t.Fatal(err)
	}
	if deals != 0 {
		t.Fatalf("a refused conversion left %d deals", deals)
	}
}

// The intake key is stored only as its hash. A new key replaces the old one, a member cannot
// make one, and a revoked key is refused.
func TestCRMIntakeKeyIsHashedReplacedAndRevoked(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-intake-key")
	ctx := t.Context()

	if _, _, err := d.CreateCRMIntakeKey(ctx, appmodel.CRMIntakeKeyRequest{TeamID: ws.teamID, CallerID: ws.memberID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("a member made an intake key: %v", err)
	}

	first, _, err := d.CreateCRMIntakeKey(ctx, appmodel.CRMIntakeKeyRequest{TeamID: ws.teamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatalf("manager makes a key: %v", err)
	}
	if len(first) != len("lk_")+64 || first[:3] != "lk_" {
		t.Fatalf("key %q has the wrong shape", first)
	}
	var stored string
	if err := d.TestSQL().QueryRowContext(ctx, `SELECT key_hash FROM crm_intake_keys WHERE team_id = ?`, ws.teamID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != HashAPIToken(first) || stored == first {
		t.Fatalf("stored %q is not the hash of the key", stored)
	}
	if team, err := d.IntakeKeyTeam(ctx, first); err != nil || team != ws.teamID {
		t.Fatalf("first key resolves to %d, %v", team, err)
	}

	second, _, err := d.CreateCRMIntakeKey(ctx, appmodel.CRMIntakeKeyRequest{TeamID: ws.teamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.IntakeKeyTeam(ctx, first); !errors.Is(err, model.ErrIntakeKeyInvalid) {
		t.Fatalf("the replaced key still works: %v", err)
	}
	if team, err := d.IntakeKeyTeam(ctx, second); err != nil || team != ws.teamID {
		t.Fatalf("second key resolves to %d, %v", team, err)
	}

	if err := d.RevokeCRMIntakeKey(ctx, appmodel.CRMIntakeKeyRequest{TeamID: ws.teamID, CallerID: ws.ownerID}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := d.IntakeKeyTeam(ctx, second); !errors.Is(err, model.ErrIntakeKeyInvalid) {
		t.Fatalf("a revoked key still works: %v", err)
	}
	if err := d.RevokeCRMIntakeKey(ctx, appmodel.CRMIntakeKeyRequest{TeamID: ws.teamID, CallerID: ws.ownerID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoking twice = %v, want not found", err)
	}
	if _, err := d.IntakeKeyTeam(ctx, ""); !errors.Is(err, model.ErrIntakeKeyInvalid) {
		t.Fatalf("an empty key = %v", err)
	}
}

// isFieldError reports whether err names the given field.
func isFieldError(err error, field string) bool {
	var fe appmodel.FieldError
	return errors.As(err, &fe) && fe.Field == field
}
