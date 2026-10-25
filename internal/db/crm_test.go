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

func countCRMStages(t *testing.T, d *DB, teamID int64) int {
	t.Helper()
	var n int
	if err := d.TestSQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM crm_stages WHERE team_id = ?`, teamID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A workspace with no stage rows reads the five defaults in order and stores nothing;
// the first write stores them.
func TestCRMDefaultStagesAppearForAFreshWorkspace(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-stages-default")
	ctx := t.Context()
	stages, err := d.ListCRMStages(ctx, appmodel.CRMStageListQuery{TeamID: ws.teamID, CallerID: ws.memberID})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{model.DealStageNew, model.DealStageWorking, model.DealStageProposal, model.DealStageWon, model.DealStageLost}
	if len(stages) != len(want) {
		t.Fatalf("stages = %+v", stages)
	}
	for i, key := range want {
		if stages[i].Key != key || stages[i].Position != i+1 {
			t.Fatalf("stage %d = %+v, want key %q at position %d", i, stages[i], key, i+1)
		}
	}
	if stages[3].Kind != model.StageKindWon || stages[4].Kind != model.StageKindLost {
		t.Fatalf("kinds of won and lost = %q, %q", stages[3].Kind, stages[4].Kind)
	}
	if n := countCRMStages(t, d, ws.teamID); n != 0 {
		t.Fatalf("a read stored %d stage rows", n)
	}
	if _, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Title: "Первая"}); err != nil {
		t.Fatal(err)
	}
	if n := countCRMStages(t, d, ws.teamID); n != len(want) {
		t.Fatalf("stage rows after the first write = %d, want %d", n, len(want))
	}
}

// A stage a manager adds is a key that deals can be created in and moved to; its key
// is derived from the name and kept unique; a member cannot add one.
func TestCRMCustomStageIsUsableByDealCreateAndMove(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-stages-custom")
	ctx := t.Context()
	stage, err := d.CreateCRMStage(ctx, appmodel.CRMStageCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Name: "Звонок клиенту", Kind: model.StageKindOpen})
	if err != nil {
		t.Fatal(err)
	}
	if stage.Key != "звонок-клиенту" || stage.Position != 6 {
		t.Fatalf("created stage = %+v, want key звонок-клиенту at position 6", stage)
	}
	again, err := d.CreateCRMStage(ctx, appmodel.CRMStageCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Name: "Звонок клиенту", Kind: model.StageKindOpen})
	if err != nil {
		t.Fatal(err)
	}
	if again.Key != "звонок-клиенту-2" {
		t.Fatalf("a repeated name got key %q, want звонок-клиенту-2", again.Key)
	}
	if _, err := d.CreateCRMStage(ctx, appmodel.CRMStageCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Name: "Чужой", Kind: model.StageKindOpen}); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("member adds a stage = %v, want ErrForbidden", err)
	}

	deal, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Title: "Звонок", Stage: stage.Key})
	if err != nil {
		t.Fatalf("deal in the custom stage: %v", err)
	}
	if deal.Stage != stage.Key {
		t.Fatalf("deal stage = %q, want %q", deal.Stage, stage.Key)
	}
	if _, err := d.MoveCRMDealStage(ctx, appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.memberID, DealID: deal.ID, Stage: again.Key}); err != nil {
		t.Fatalf("move to the custom stage: %v", err)
	}
	if _, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: ws.teamID, CallerID: ws.memberID, Title: "Нет", Stage: "нет-такого"}); !errors.Is(err, model.ErrInvalidDealStage) {
		t.Fatalf("unknown stage key = %v, want ErrInvalidDealStage", err)
	}
}

// A stage with deals is refused with a field error on "stage"; once the deals have
// moved on, the same stage goes.
func TestCRMStageWithDealsCannotBeDeleted(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-stages-delete")
	ctx := t.Context()
	stage, err := d.CreateCRMStage(ctx, appmodel.CRMStageCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Name: "Ожидание", Kind: model.StageKindOpen})
	if err != nil {
		t.Fatal(err)
	}
	deal, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Ждём", Stage: stage.Key})
	if err != nil {
		t.Fatal(err)
	}
	err = d.DeleteCRMStage(ctx, appmodel.CRMStageDeleteRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Key: stage.Key})
	var field appmodel.FieldError
	if !errors.As(err, &field) || field.Field != "stage" {
		t.Fatalf("delete a stage with deals = %v, want a field error on stage", err)
	}
	if _, err := d.MoveCRMDealStage(ctx, appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.ownerID, DealID: deal.ID, Stage: model.DealStageWorking}); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteCRMStage(ctx, appmodel.CRMStageDeleteRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Key: stage.Key}); err != nil {
		t.Fatalf("delete the emptied stage: %v", err)
	}
	stages, err := d.ListCRMStages(ctx, appmodel.CRMStageListQuery{TeamID: ws.teamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 5 {
		t.Fatalf("stages after the delete = %+v, want the five defaults", stages)
	}
}

// The lost kind belongs to the stage, not to a key: a custom lost stage needs a reason
// when a deal enters it, and a deal that leaves it clears the reason.
func TestCRMLostKindOfACustomStageNeedsAReason(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-stages-lost")
	ctx := t.Context()
	lost, err := d.CreateCRMStage(ctx, appmodel.CRMStageCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Name: "Отказ клиента", Kind: model.StageKindLost})
	if err != nil {
		t.Fatal(err)
	}
	deal, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Сделка", Stage: model.DealStageWorking})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Сразу", Stage: lost.Key}); !errors.Is(err, model.ErrLostReasonRequired) {
		t.Fatalf("a new deal in a lost stage = %v, want ErrLostReasonRequired", err)
	}
	if _, err := d.MoveCRMDealStage(ctx, appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.ownerID, DealID: deal.ID, Stage: lost.Key}); !errors.Is(err, model.ErrLostReasonRequired) {
		t.Fatalf("lost without a reason = %v, want ErrLostReasonRequired", err)
	}
	moved, err := d.MoveCRMDealStage(ctx, appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.ownerID, DealID: deal.ID, Stage: lost.Key, LostReason: "дорого"})
	if err != nil {
		t.Fatal(err)
	}
	if moved.LostReason != "дорого" || moved.StageKind != model.StageKindLost {
		t.Fatalf("after lost = %+v", moved)
	}
	back, err := d.MoveCRMDealStage(ctx, appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.ownerID, DealID: deal.ID, Stage: model.DealStageWorking})
	if err != nil {
		t.Fatal(err)
	}
	if back.LostReason != "" {
		t.Fatalf("a deal that left the lost stage keeps %q", back.LostReason)
	}
}

// Overdue applies to open stages only: a deal in a won stage with a past next step is
// not on the list, while an open deal with the same date is.
func TestCRMOverdueIgnoresAWonKindStage(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-stages-overdue")
	ctx := t.Context()
	won, err := d.CreateCRMStage(ctx, appmodel.CRMStageCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Name: "Подписан", Kind: model.StageKindWon})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Подписан", Stage: won.Key, NextStepAt: "2026-10-20"})
	if err != nil {
		t.Fatal(err)
	}
	open, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Открыта", NextStepAt: "2026-10-20"})
	if err != nil {
		t.Fatal(err)
	}
	if signed.Overdue("2026-10-24") {
		t.Fatal("a deal in a won stage is overdue")
	}
	list, err := d.ListCRMDeals(ctx, appmodel.CRMDealListQuery{TeamID: ws.teamID, CallerID: ws.ownerID, Overdue: true, Today: "2026-10-24"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != open.ID {
		t.Fatalf("overdue list = %+v, want only the open deal", list)
	}
}

// A workspace keeps an open, a won and a lost stage. A stage is moved by position and
// the others shift to keep the positions contiguous.
func TestCRMPipelineKeepsOneStageOfEachKind(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-stages-kinds")
	ctx := t.Context()
	owner := appmodel.CRMStageDeleteRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Key: model.DealStageLost}
	var field appmodel.FieldError
	if err := d.DeleteCRMStage(ctx, owner); !errors.As(err, &field) || field.Field != "kind" {
		t.Fatalf("delete the only lost stage = %v, want a field error on kind", err)
	}
	kind := model.StageKindOpen
	if _, err := d.UpdateCRMStage(ctx, appmodel.CRMStageUpdateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Key: model.DealStageWon, Kind: &kind}); !errors.As(err, &field) || field.Field != "kind" {
		t.Fatalf("re-kind the only won stage = %v, want a field error on kind", err)
	}

	first := 1
	moved, err := d.UpdateCRMStage(ctx, appmodel.CRMStageUpdateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Key: model.DealStageLost, Position: &first})
	if err != nil {
		t.Fatal(err)
	}
	stages, err := d.ListCRMStages(ctx, appmodel.CRMStageListQuery{TeamID: ws.teamID, CallerID: ws.ownerID})
	if err != nil {
		t.Fatal(err)
	}
	if moved.Position != 1 || stages[0].Key != model.DealStageLost || stages[1].Key != model.DealStageNew || stages[4].Position != 5 {
		t.Fatalf("after the move = %+v", stages)
	}

	extra, err := d.CreateCRMStage(ctx, appmodel.CRMStageCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Name: "Оплачен", Kind: model.StageKindWon})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteCRMStage(ctx, appmodel.CRMStageDeleteRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Key: model.DealStageWon}); err != nil {
		t.Fatalf("delete a won stage while another is left: %v", err)
	}
	if extra.Kind != model.StageKindWon {
		t.Fatalf("extra kind = %q", extra.Kind)
	}
}

// The fixed stage CHECK is gone: a key that is not a workspace stage is accepted by the
// schema, and deals stored before the change read as they did.
func TestCRMDealsReadableWithoutTheFixedStageCheck(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-stages-constraint")
	ctx := t.Context()
	deal, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Старая", Stage: model.DealStageWorking, NextStepAt: "2026-10-20"})
	if err != nil {
		t.Fatal(err)
	}
	var checks int
	if err := d.TestSQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_constraint WHERE conrelid = 'crm_deals'::regclass AND conname = 'crm_deals_stage_check'`).Scan(&checks); err != nil {
		t.Fatal(err)
	}
	if checks != 0 {
		t.Fatal("crm_deals still has the fixed stage check")
	}
	if _, err := d.TestSQL().ExecContext(ctx, `UPDATE crm_deals SET stage = 'своя' WHERE id = ?`, deal.ID); err != nil {
		t.Fatalf("the schema refuses a stage key outside the defaults: %v", err)
	}
	got, err := d.GetCRMDeal(ctx, appmodel.CRMDealLookupQuery{TeamID: ws.teamID, CallerID: ws.ownerID, DealID: deal.ID})
	if err != nil {
		t.Fatalf("read a deal with a key the workspace does not have: %v", err)
	}
	if got.Stage != "своя" || got.Overdue("2026-10-24") {
		t.Fatalf("deal = %+v, want its stored key and no overdue flag without a stage kind", got)
	}
}

// The project link runs the deal rules again under the lock: a deal that left the won
// stage is refused, and a deal that already has a project is refused a second link.
func TestCRMProjectLinkChecksTheDealAgain(t *testing.T) {
	d := openTestDB(t)
	ws := seedRichWorkspace(t, d, "crm-link")
	ctx := t.Context()

	deal, err := d.CreateCRMDeal(ctx, appmodel.CRMDealCreateRequest{
		TeamID: ws.teamID, CallerID: ws.ownerID, Title: "Лендинг", Stage: model.DealStageWon, ClientID: ws.clientID,
	})
	if err != nil {
		t.Fatal(err)
	}
	project, err := d.CreateProjectWithBilling(ctx, appmodel.ProjectCreateRequest{TeamID: ws.teamID, CallerID: ws.ownerID, Name: "Лендинг"})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	link := appmodel.CRMDealProjectLinkRequest{TeamID: ws.teamID, CallerID: ws.ownerID, DealID: deal.ID, ProjectID: project.ID}

	if _, err := d.MoveCRMDealStage(ctx, appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.ownerID, DealID: deal.ID, Stage: model.DealStageWorking}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.LinkCRMDealProject(ctx, link); err == nil {
		t.Fatal("a deal that left the won stage was linked")
	} else {
		wantFieldErr(t, err, "stage")
	}

	if _, err := d.MoveCRMDealStage(ctx, appmodel.CRMDealStageRequest{TeamID: ws.teamID, CallerID: ws.ownerID, DealID: deal.ID, Stage: model.DealStageWon}); err != nil {
		t.Fatal(err)
	}
	linked, err := d.LinkCRMDealProject(ctx, link)
	if err != nil || linked.ProjectID != project.ID {
		t.Fatalf("link = %+v, %v; want project %d", linked, err, project.ID)
	}
	if _, err := d.LinkCRMDealProject(ctx, link); err == nil {
		t.Fatal("a second link was accepted")
	} else {
		wantFieldErr(t, err, "project")
	}
}

func wantFieldErr(t *testing.T, err error, field string) {
	t.Helper()
	var fe appmodel.FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Fatalf("error = %v, want a field error on %q", err, field)
	}
}
