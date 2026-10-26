package db

import (
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// fieldOf names the input a refused write points at, or "" when the error is not a field error.
func fieldOf(err error) string {
	var field appmodel.FieldError
	if errors.As(err, &field) {
		return field.Field
	}
	return ""
}

// A deal's owner is a member of the workspace, and its contact is a contact of the deal's
// own client. The transaction checks both, so a route that forgets to is not the only guard.
func TestCRMDealOwnerAndContactBelongToTheWorkspace(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-guards-deal")
	outsider := seedRichWorkspace(t, d, "crm-guards-outsider")
	second, err := d.CreateClient(t.Context(), appmodel.ClientCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Name: "Вторая"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := d.CreateCRMDeal(t.Context(), appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Чужой владелец", Stage: model.DealStageNew, OwnerID: outsider.ownerID,
	}); fieldOf(err) != "owner_id" {
		t.Fatalf("owner from another workspace = %v, want field owner_id", err)
	}

	deal, err := d.CreateCRMDeal(t.Context(), appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Сайт", Stage: model.DealStageNew, ClientID: ws.clientID,
	})
	if err != nil {
		t.Fatal(err)
	}
	stranger := outsider.ownerID
	if _, err := d.UpdateCRMDeal(t.Context(), appmodel.CRMDealUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Manager: true, DealID: deal.ID, OwnerID: &stranger,
	}); fieldOf(err) != "owner_id" {
		t.Fatalf("update to an owner from another workspace = %v, want field owner_id", err)
	}

	contact, err := d.CreateCRMContact(t.Context(), appmodel.CRMContactCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, ClientID: second.ID, Name: "Анна",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateCRMDeal(t.Context(), appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Не того клиента", Stage: model.DealStageNew, ClientID: ws.clientID, ContactID: contact.ID,
	}); fieldOf(err) != "contact_id" {
		t.Fatalf("contact of another client on a new deal = %v, want field contact_id", err)
	}
	withContact := contact.ID
	if _, err := d.UpdateCRMDeal(t.Context(), appmodel.CRMDealUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Manager: true, DealID: deal.ID, ContactID: &withContact,
	}); fieldOf(err) != "contact_id" {
		t.Fatalf("contact of another client on an update = %v, want field contact_id", err)
	}
	missing := int64(999999)
	if _, err := d.UpdateCRMDeal(t.Context(), appmodel.CRMDealUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Manager: true, DealID: deal.ID, ContactID: &missing,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing contact on an update = %v, want not found", err)
	}
}

// Deleting a contact follows the spec: a manager, or the member who created it. The deals
// that named the contact keep the deal and lose the link.
func TestCRMContactDeleteFollowsTheCreatorRule(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-guards-contact")
	ctx := t.Context()

	byOwner, err := d.CreateCRMContact(ctx, appmodel.CRMContactCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, ClientID: ws.clientID, Name: "Руководителя",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteCRMContact(ctx, appmodel.CRMContactDeleteRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ContactID: byOwner.ID,
	}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member deletes a contact made by the manager = %v, want ErrForbidden", err)
	}

	byMember, err := d.CreateCRMContact(ctx, appmodel.CRMContactCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ClientID: ws.clientID, Name: "Участника",
	})
	if err != nil {
		t.Fatal(err)
	}
	deal, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Со связью", Stage: model.DealStageNew,
		ClientID: ws.clientID, ContactID: byMember.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteCRMContact(ctx, appmodel.CRMContactDeleteRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ContactID: byMember.ID,
	}); err != nil {
		t.Fatalf("creator deletes own contact: %v", err)
	}
	if err := d.DeleteCRMContact(ctx, appmodel.CRMContactDeleteRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ContactID: byMember.ID,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete of the same contact = %v, want not found", err)
	}
	kept, err := d.GetCRMDeal(ctx, appmodel.CRMDealLookupQuery{TeamID: ws.teamID, CallerID: ws.ownerID, DealID: deal.ID})
	if err != nil {
		t.Fatal(err)
	}
	if kept.ContactID != 0 || kept.Title != "Со связью" {
		t.Fatalf("deal after its contact was deleted = contact %d, title %q; want the deal kept with no contact", kept.ContactID, kept.Title)
	}

	again, err := d.CreateCRMContact(ctx, appmodel.CRMContactCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ClientID: ws.clientID, Name: "Ещё один",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteCRMContact(ctx, appmodel.CRMContactDeleteRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, ContactID: again.ID,
	}); err != nil {
		t.Fatalf("manager deletes a member's contact: %v", err)
	}
}

// A stage becomes lost only when it holds no deals, because a deal in a lost stage needs its
// reason. A repeated stage name is allowed: the key gets a suffix, as the spec of the stages has it.
func TestCRMStageNamesAndLostKindFollowTheDeals(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-guards-stage")
	ctx := t.Context()

	busy, err := d.CreateCRMStage(ctx, appmodel.CRMStageCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Name: "Переговоры", Kind: model.StageKindOpen,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "В переговорах", Stage: busy.Key, ClientID: ws.clientID,
	}); err != nil {
		t.Fatal(err)
	}
	lost := model.StageKindLost
	if _, err := d.UpdateCRMStage(ctx, appmodel.CRMStageUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Key: busy.Key, Kind: &lost,
	}); fieldOf(err) != "kind" {
		t.Fatalf("a stage with deals becomes lost = %v, want field kind", err)
	}

	empty, err := d.CreateCRMStage(ctx, appmodel.CRMStageCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Name: "Пустая", Kind: model.StageKindOpen,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpdateCRMStage(ctx, appmodel.CRMStageUpdateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Key: empty.Key, Kind: &lost,
	}); err != nil {
		t.Fatalf("an empty stage becomes lost: %v", err)
	}
}
