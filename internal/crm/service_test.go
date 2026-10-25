package crm

import (
	"context"
	"errors"
	"testing"

	"github.com/aa-blinov/paratrack/internal/appmodel"
	"github.com/aa-blinov/paratrack/internal/model"
)

// fakeStore records the deal create requests that reach the persistence layer and
// serves the deal list it is given.
type fakeStore struct {
	Store
	created []appmodel.CRMDealCreateRequest
	deals   []model.CRMDeal
}

func (f *fakeStore) CreateCRMDeal(_ context.Context, request appmodel.CRMDealCreateRequest) (model.CRMDeal, error) {
	f.created = append(f.created, request)
	return model.CRMDeal{Title: request.Title, Stage: request.Stage}, nil
}

func (f *fakeStore) ListCRMDeals(context.Context, appmodel.CRMDealListQuery) ([]model.CRMDeal, error) {
	return f.deals, nil
}

func newTestService(t *testing.T, store Store) *Service {
	t.Helper()
	svc, err := New(Dependencies{Store: store})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func wantField(t *testing.T, err error, field string) {
	t.Helper()
	var fe appmodel.FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Fatalf("error = %v, want a field error on %q", err, field)
	}
}

func TestCreateDealTrimsDefaultsAndRefusesBadInput(t *testing.T) {
	store := &fakeStore{}
	svc := newTestService(t, store)
	ctx := context.Background()

	if _, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "   "}); err == nil {
		t.Fatal("blank title was accepted")
	} else {
		wantField(t, err, "deal title")
	}
	if _, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "Сайт", Probability: 101}); err == nil {
		t.Fatal("probability above 100 was accepted")
	} else {
		wantField(t, err, "probability")
	}
	if _, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "Сайт", CloseDate: "2026-13-01"}); err == nil {
		t.Fatal("impossible close date was accepted")
	} else {
		wantField(t, err, "close date")
	}
	if _, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "Сайт", Currency: "rubles"}); err == nil {
		t.Fatal("long currency was accepted")
	} else {
		wantField(t, err, "currency")
	}
	if len(store.created) != 0 {
		t.Fatalf("a refused deal reached the store: %+v", store.created)
	}

	deal, err := svc.CreateDeal(ctx, appmodel.CRMDealCreateRequest{TeamID: 1, Title: "  Сайт  ", Currency: "rub", NextStepAt: "2026-10-30"})
	if err != nil {
		t.Fatal(err)
	}
	if deal.Title != "Сайт" || len(store.created) != 1 {
		t.Fatalf("created = %+v, deal = %+v", store.created, deal)
	}
	if got := store.created[0]; got.Stage != model.DealStageNew || got.Currency != "RUB" {
		t.Fatalf("stage and currency = %q, %q; want new and RUB", got.Stage, got.Currency)
	}
}

func TestMoveDealStageRefusesAnUnknownStage(t *testing.T) {
	svc := newTestService(t, &fakeStore{})
	_, err := svc.MoveDealStage(context.Background(), appmodel.CRMDealStageRequest{TeamID: 1, Stage: "closed"})
	if !errors.Is(err, model.ErrInvalidDealStage) {
		t.Fatalf("unknown stage = %v, want ErrInvalidDealStage", err)
	}
}

func TestDealsOverdueNeedsTheWorkspaceDay(t *testing.T) {
	svc := newTestService(t, &fakeStore{})
	_, err := svc.Deals(context.Background(), appmodel.CRMDealListQuery{TeamID: 1, Overdue: true})
	wantField(t, err, "today")
}

func TestPipelineCountsEveryStageAndTheOverdueOpenDeals(t *testing.T) {
	store := &fakeStore{deals: []model.CRMDeal{
		{Stage: model.DealStageNew, NextStepAt: "2026-10-01"},
		{Stage: model.DealStageNew, NextStepAt: "2026-10-30"},
		{Stage: model.DealStageWon, NextStepAt: "2026-10-01"},
		{Stage: model.DealStageLost},
	}}
	svc := newTestService(t, store)
	pipeline, err := svc.Pipeline(context.Background(), appmodel.CRMDealListQuery{TeamID: 1, Today: "2026-10-24"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pipeline.Stages) != len(model.DealStages) {
		t.Fatalf("stages = %+v", pipeline.Stages)
	}
	counts := map[string]int{}
	for _, stage := range pipeline.Stages {
		counts[stage.Stage] = stage.Count
	}
	if counts[model.DealStageNew] != 2 || counts[model.DealStageWon] != 1 || counts[model.DealStageLost] != 1 || counts[model.DealStageProposal] != 0 {
		t.Fatalf("counts = %+v", counts)
	}
	if pipeline.Overdue != 1 {
		t.Fatalf("overdue = %d, want only the open deal with a past next step", pipeline.Overdue)
	}
}

func TestLogActivityDefaultsToANoteAndNeedsAnAnchor(t *testing.T) {
	svc := newTestService(t, &fakeStore{})
	ctx := context.Background()
	if _, err := svc.LogActivity(ctx, appmodel.CRMActivityCreateRequest{TeamID: 1, Body: "созвон"}); err == nil {
		t.Fatal("an activity without a client or deal was accepted")
	} else {
		wantField(t, err, "client or deal")
	}
	if _, err := svc.LogActivity(ctx, appmodel.CRMActivityCreateRequest{TeamID: 1, ClientID: 3}); err == nil {
		t.Fatal("an empty activity was accepted")
	} else {
		wantField(t, err, "body")
	}
}

func TestNewNeedsAStore(t *testing.T) {
	if _, err := New(Dependencies{}); !errors.Is(err, ErrIncompleteDependencies) {
		t.Fatalf("New without a store = %v", err)
	}
}
