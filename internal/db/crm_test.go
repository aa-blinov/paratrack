package db

import (
	"errors"
	"testing"
	"time"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// A member records a deal and moves it; a second member cannot change it; a manager
// moves it to another owner; a lost deal needs a reason; overdue deals list on their own.
func TestCRMDealRulesAndOverdue(t *testing.T) {
	d := openTestDB(t)
	d.now = func() time.Time { return time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC) }
	ws := seedRichWorkspace(t, d, "crm-deals")

	deal, err := d.CreateCRMDeal(t.Context(), appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, Title: "Сайт для клиента", Stage: model.DealStageNew,
		ClientID: ws.clientID, AmountCents: 250000, Currency: "RUB", Probability: 30, NextStepAt: "2026-10-20",
	})
	if err != nil {
		t.Fatalf("member records a deal: %v", err)
	}
	if deal.OwnerID != ws.memberID {
		t.Fatalf("owner = %d, want the member who recorded it", deal.OwnerID)
	}

	if !deal.Overdue("2026-10-24") {
		t.Fatal("an open deal whose next step is past is not overdue")
	}
	overdue, err := d.ListCRMDeals(t.Context(), appmodel.CRMDealListQuery{TeamID: ws.teamID, CallerID: ws.memberID, Overdue: true, Today: "2026-10-24"})
	if err != nil {
		t.Fatal(err)
	}
	if len(overdue) != 1 || overdue[0].ID != deal.ID {
		t.Fatalf("overdue list = %+v, want the one deal", overdue)
	}

	if _, err := d.MoveCRMDealStage(t.Context(), appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.memberID, DealID: deal.ID, Stage: model.DealStageLost}); !errors.Is(err, model.ErrLostReasonRequired) {
		t.Fatalf("lost without a reason = %v, want ErrLostReasonRequired", err)
	}
	moved, err := d.MoveCRMDealStage(t.Context(), appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.memberID, DealID: deal.ID, Stage: model.DealStageLost, LostReason: "бюджет"})
	if err != nil {
		t.Fatalf("lost with a reason: %v", err)
	}
	if moved.Stage != model.DealStageLost || moved.LostReason != "бюджет" {
		t.Fatalf("after lost = %+v", moved)
	}
	if moved.Overdue("2026-10-24") {
		t.Fatal("a lost deal stays on the overdue list")
	}

	reopened, err := d.MoveCRMDealStage(t.Context(), appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.memberID, DealID: deal.ID, Stage: model.DealStageWorking})
	if err != nil {
		t.Fatalf("back to working: %v", err)
	}
	if reopened.LostReason != "" {
		t.Fatalf("a reopened deal keeps its lost reason %q", reopened.LostReason)
	}

	if _, err := d.MoveCRMDealStage(t.Context(), appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.ownerID, DealID: deal.ID, Stage: "won"}); err != nil {
		// The owner of the workspace is a manager and may move any deal.
		t.Fatalf("manager moves the member's deal: %v", err)
	}
	managerDeal, err := d.CreateCRMDeal(t.Context(), appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Руководителя", Stage: model.DealStageNew,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MoveCRMDealStage(t.Context(), appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.memberID, DealID: managerDeal.ID, Stage: model.DealStageWorking}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member moves a deal it does not own = %v, want ErrForbidden", err)
	}

	other, err := d.CreateCRMDeal(t.Context(), appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Поддержка", Stage: model.DealStageNew, OwnerID: ws.memberID,
	})
	if err != nil {
		t.Fatalf("manager assigns a deal to a member: %v", err)
	}
	ownOwner := ws.memberID
	if _, err := d.UpdateCRMDeal(t.Context(), appmodel.CRMDealUpdateRequest{TeamID: ws.teamID, CallerID: ws.memberID, DealID: other.ID, OwnerID: &ownOwner}); err != nil {
		t.Fatalf("member keeps its own deal: %v", err)
	}
	ownerID := ws.ownerID
	if _, err := d.UpdateCRMDeal(t.Context(), appmodel.CRMDealUpdateRequest{TeamID: ws.teamID, CallerID: ws.memberID, DealID: other.ID, OwnerID: &ownerID}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member hands its deal over = %v, want ErrForbidden", err)
	}
}

// A deal of another workspace is not found, and a member cannot create a deal for a
// colleague.
func TestCRMDealsStayInTheirWorkspace(t *testing.T) {
	d := openTestDB(t)
	mine := seedRichWorkspace(t, d, "crm-scope-mine")
	theirs := seedRichWorkspace(t, d, "crm-scope-theirs")

	deal, err := d.CreateCRMDeal(t.Context(), appmodel.CRMDealCreateRequest{
		TeamID: theirs.teamID, CallerID: theirs.ownerID, Title: "Чужая", Stage: model.DealStageNew,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetCRMDeal(t.Context(), appmodel.CRMDealLookupQuery{TeamID: mine.teamID, CallerID: mine.ownerID, DealID: deal.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a deal of another workspace = %v, want not found", err)
	}
	if _, err := d.CreateCRMDeal(t.Context(), appmodel.CRMDealCreateRequest{
		TeamID: mine.teamID, CallerID: mine.memberID, Title: "Для коллеги", Stage: model.DealStageNew, OwnerID: mine.ownerID,
	}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member creates a deal for a colleague = %v, want ErrForbidden", err)
	}
	if _, err := d.CreateCRMContact(t.Context(), appmodel.CRMContactCreateRequest{
		TeamID: mine.teamID, CallerID: mine.memberID, ClientID: theirs.clientID, Name: "Чужой клиент",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("contact for a client of another workspace = %v, want not found", err)
	}
}

// A call is an activity on the deal's client; it does not touch time tracking.
func TestCRMActivityOnADealKeepsTheClient(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-activity")
	deal, err := d.CreateCRMDeal(t.Context(), appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, Title: "Звонок", Stage: model.DealStageNew, ClientID: ws.clientID,
	})
	if err != nil {
		t.Fatal(err)
	}
	act, err := d.CreateCRMActivity(t.Context(), appmodel.CRMActivityCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, DealID: deal.ID, Kind: "call", Body: "обсудили бюджет", Result: "перезвонить в пятницу",
	})
	if err != nil {
		t.Fatalf("log a call on a deal: %v", err)
	}
	if act.ClientID != ws.clientID {
		t.Fatalf("activity client = %d, want the deal's client %d", act.ClientID, ws.clientID)
	}
	list, err := d.ListCRMActivities(t.Context(), appmodel.CRMActivityListQuery{TeamID: ws.teamID, CallerID: ws.memberID, ClientID: ws.clientID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Kind != "call" {
		t.Fatalf("client timeline = %+v, want the one call", list)
	}
	if _, err := d.CreateCRMActivity(t.Context(), appmodel.CRMActivityCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, ClientID: ws.clientID, Kind: "letter",
	}); !errors.Is(err, model.ErrInvalidActivityKind) {
		t.Fatalf("unknown kind = %v, want ErrInvalidActivityKind", err)
	}
}

// A client keeps several contacts, and a won deal creates no project and no time
// session: the deal is a sale, not work.
func TestCRMClientKeepsSeveralContactsAndWonDealStaysOutOfTime(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-won-time")
	ctx := t.Context()
	for _, c := range []appmodel.CRMContactCreateRequest{
		{TeamID: ws.teamID, CallerID: ws.memberID, ClientID: ws.clientID, Name: "Анна", Role: "закупки"},
		{TeamID: ws.teamID, CallerID: ws.memberID, ClientID: ws.clientID, Name: "Борис", Role: "директор"},
	} {
		if _, err := d.CreateCRMContact(ctx, c); err != nil {
			t.Fatalf("contact %s: %v", c.Name, err)
		}
	}
	contacts, err := d.ListCRMContacts(ctx, appmodel.CRMContactListQuery{TeamID: ws.teamID, CallerID: ws.memberID, ClientID: ws.clientID})
	if err != nil {
		t.Fatal(err)
	}
	if len(contacts) != 2 {
		t.Fatalf("contacts of the client = %+v, want both", contacts)
	}

	count := func(query string) int {
		t.Helper()
		var n int
		if err := d.TestSQL().QueryRowContext(ctx, query, ws.teamID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	projectsBefore := count(`SELECT COUNT(*) FROM projects WHERE team_id = ?`)
	sessionsBefore := count(`SELECT COUNT(*) FROM sessions WHERE team_id = ?`)

	deal, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.memberID, Title: "Продажа", Stage: model.DealStageProposal, ClientID: ws.clientID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.MoveCRMDealStage(ctx, appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.memberID, DealID: deal.ID, Stage: model.DealStageWon}); err != nil {
		t.Fatalf("won: %v", err)
	}
	if got := count(`SELECT COUNT(*) FROM projects WHERE team_id = ?`); got != projectsBefore {
		t.Fatalf("a won deal created projects: %d → %d", projectsBefore, got)
	}
	if got := count(`SELECT COUNT(*) FROM sessions WHERE team_id = ?`); got != sessionsBefore {
		t.Fatalf("a won deal created sessions: %d → %d", sessionsBefore, got)
	}
}
